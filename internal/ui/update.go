package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"tui-commander/internal/archive"
	"tui-commander/internal/config"
	"tui-commander/internal/openwith"
	"tui-commander/internal/theme"
	"tui-commander/internal/trash"
	"tui-commander/internal/vfs"
)

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case theme.ChangedMsg:
		applyTheme(message.Palette)
		return m, theme.Watch()

	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		if err := m.resizeTerminal(); err != nil {
			m.setStatus("Resize terminal: "+err.Error(), true)
		}
		for index := range m.panes {
			m.panes[index].clamp(m.visibleRows())
		}
		return m, nil
	case terminalOutputMsg:
		if message.session != m.terminal {
			return m, nil
		}
		if len(message.data) > 0 {
			_, _ = message.session.emulator.Write(message.data)
		}
		if message.err != nil {
			_ = message.session.emulator.Close()
			return m, nil
		}
		return m, readTerminalCmd(message.session)
	case terminalExitedMsg:
		if message.session == m.terminal {
			message.session.exited = true
			m.setStatus("Terminal exited · toggle F9 off and on to restart", message.err != nil)
		}
		return m, nil
	case fuzzyFinishedMsg:
		return m.handleFuzzyFinished(message)
	case syncScannedMsg:
		m.stopBusy()
		return m.handleSyncScanned(message)
	case syncFinishedMsg:
		m.stopBusy()
		return m.handleSyncFinished(message)
	case paneLoadedMsg:
		pane := message.pane
		if pane.location.Path != message.path {
			return m, nil
		}
		pane.loading, pane.err = false, message.err
		if message.err == nil {
			pane.entries = message.entries
			pane.git = message.git
			pane.selected = make(map[string]bool)
			if pane.revealPath != "" {
				for index, entry := range pane.visibleEntries() {
					if entry.Path == pane.revealPath {
						pane.cursor = index
						break
					}
				}
				pane.revealPath = ""
			}
			pane.clamp(m.visibleRows())
		} else {
			m.setStatus(message.err.Error(), true)
		}
		return m, nil
	case connectedMsg:
		m.stopBusy()
		if message.err != nil {
			var untrusted *vfs.UntrustedCertificateError
			if errors.As(message.err, &untrusted) {
				m.pendingTrust = certificateTrustState{
					index: message.index, raw: message.raw, password: message.password,
					passwordSaved: message.passwordSaved, fingerprint: untrusted.Fingerprint,
				}
				m.modal = modalConfirm
				m.confirm = confirmState{
					action: confirmTrustCertificate,
					title: fmt.Sprintf(
						"Untrusted certificate %q\nSHA-256:\n%s\n\nTrust this exact certificate and reconnect?",
						untrusted.CommonName, displayCertificateFingerprint(untrusted.Fingerprint),
					),
				}
				m.setStatus("Certificate confirmation required", true)
				return m, nil
			}
			m.setStatus("Connection failed: "+message.err.Error(), true)
			return m, nil
		}
		m.pendingTrust = certificateTrustState{}
		m.backends = append(m.backends, message.location.Backend)
		previous := m.panes[message.index]
		localReturn := previous.localReturn
		if previous.location.Backend.ID() == "local" {
			localReturn = previous.location.Path
		}
		connectedPane := &pane{
			location: message.location, selected: make(map[string]bool), loading: true,
			showHidden: m.panes[message.index].showHidden, password: message.password,
			passwordSaved: message.passwordSaved, localReturn: localReturn,
		}
		m.panes[message.index] = connectedPane
		m.tabs[message.index][m.activeTab[message.index]] = connectedPane
		m.setStatus("Connected to "+message.location.Backend.Label(), false)
		return m, m.loadPaneCmd(message.index)
	case operationDoneMsg:
		m.stopBusy()
		if message.err != nil {
			m.setStatus(message.description+": "+message.err.Error(), true)
		} else {
			m.setStatus(message.description+" complete", false)
		}
		for index := range m.panes {
			m.panes[index].loading = true
		}
		return m, tea.Batch(m.loadPaneCmd(0), m.loadPaneCmd(1))
	case applicationsLoadedMsg:
		m.stopBusy()
		if message.err != nil {
			m.setStatus(message.err.Error(), true)
			return m, nil
		}
		if len(message.items) == 0 {
			m.setStatus("No desktop applications are available for "+message.mimeType, true)
			return m, nil
		}
		m.modal = modalApplications
		m.applications = applicationState{items: message.items, path: message.path, mimeType: message.mimeType, remoteFile: message.remoteFile}
		return m, nil
	case remoteDownloadedMsg:
		m.stopBusy()
		if message.err != nil {
			m.setStatus("Download for opening failed: "+message.err.Error(), true)
			return m, nil
		}
		if !message.file.readOnly {
			m.remoteFiles = append(m.remoteFiles, message.file)
			sortRemoteFiles(m.remoteFiles)
		}
		if message.chooser {
			m.busy, m.busyLabel = true, "Finding compatible applications"
			return m, tea.Batch(applicationsCmd(message.file.localPath, message.file), busyTickCmd())
		}
		return m, m.launchDefault(message.file.localPath, message.file)
	case remoteUploadedMsg:
		m.stopBusy()
		if message.err != nil {
			m.setStatus("Upload failed: "+message.err.Error(), true)
			return m, nil
		}
		if info, err := os.Stat(message.file.localPath); err == nil {
			message.file.stamp, message.file.size, message.file.dirty = info.ModTime(), info.Size(), false
		}
		m.setStatus("Uploaded edited file to "+message.file.remotePath, false)
		return m, m.loadPaneCmd(m.focus)
	case credentialResolvedMsg:
		m.stopBusy()
		if message.err != nil {
			m.credentialMaster, m.credentialUntil = "", time.Time{}
			m.modal = modalBookmarks
			m.setStatus("gopass: "+message.err.Error(), true)
			return m, nil
		}
		m.credentialMaster = message.request.master
		m.credentialUntil = time.Now().Add(15 * time.Minute)
		m.pendingCredential = credentialRequest{}
		location := locationWithCredentialUsername(message.request.location, message.username)
		m.busy, m.busyLabel = true, "Connecting"
		return m, tea.Batch(connectCmd(message.request.index, location, message.password, false), busyTickCmd())
	case appClosedMsg:
		if message.err != nil {
			m.setStatus("Open application: "+message.err.Error(), true)
		} else {
			m.setStatus("Application launched", false)
		}
		return m, nil
	case clipboardCopiedMsg:
		if message.err != nil {
			m.setStatus("Copy path: "+message.err.Error(), true)
		} else {
			m.setStatus("Copied path: "+message.path, false)
		}
		return m, nil
	case busyTickMsg:
		if !m.busy {
			return m, nil
		}
		m.busyFrame++
		if m.progressCh != nil {
			for {
				select {
				case progress := <-m.progressCh:
					m.progress = progress
				default:
					return m, busyTickCmd()
				}
			}
		}
		return m, busyTickCmd()
	case watchTickMsg:
		for _, file := range m.remoteFiles {
			info, err := os.Stat(file.localPath)
			if err == nil && (info.Size() != file.size || !info.ModTime().Equal(file.stamp)) {
				file.dirty = true
			}
		}
		if dirty := m.firstDirtyRemote(); dirty != nil && !m.busy && m.modal == modalNone {
			m.setStatus("Remote edit changed: press Alt+U to upload "+filepath.Base(dirty.remotePath), false)
		}
		return m, watchTickCmd()
	case tea.MouseMsg:
		if m.terminalVisible {
			return m, nil
		}
		if m.modal == modalNone && !m.busy {
			return m.handleMouse(tea.MouseEvent(message))
		}
		return m, nil
	}

	key, ok := message.(tea.KeyMsg)
	if !ok {
		if direction := zoomKey(message); direction != 0 {
			return m, m.zoomFont(direction)
		}
		if m.modal == modalPrompt {
			if editKey := promptUnknownControlKey(message); editKey != "" {
				if !m.prompt.checkboxFocus {
					editPromptText(&m.prompt, editKey)
				}
				return m, nil
			}
		}
		return m, nil
	}
	if key.String() == "f9" && m.modal == modalNone && !m.busy {
		return m, m.toggleTerminal()
	}
	if m.modal != modalNone {
		return m.handleModalKey(key)
	}
	if m.busy {
		if key.String() == "esc" && m.cancel != nil {
			m.cancel()
			m.setStatus("Cancelling operation…", false)
		}
		return m, nil
	}
	if m.terminalVisible {
		m.sendTerminalKey(key)
		return m, nil
	}
	return m.handleMainKey(key)
}

func (m *Model) handleMouse(mouse tea.MouseEvent) (tea.Model, tea.Cmd) {
	previousFocus := m.focus
	index := 0
	if mouse.X >= m.width/2 {
		index = 1
	}
	m.focus = index
	if mouse.Action == tea.MouseActionPress && previousFocus != index {
		m.persistSessionState()
	}
	pane := m.panes[index]
	if mouse.Button == tea.MouseButtonLeft && mouse.Action == tea.MouseActionPress && mouse.Y == 2 {
		contentWidth := max(1, m.width-2)
		leftWidth := max(20, (contentWidth-1)/2)
		paneWidth, paneStart := leftWidth, 1
		if index == 1 {
			paneWidth = max(20, contentWidth-leftWidth-1)
			paneStart = leftWidth + 2
		}
		if tabIndex := m.tabAt(index, paneWidth, mouse.X-paneStart); tabIndex >= 0 {
			return m, m.activateTab(index, tabIndex)
		}
		return m, nil
	}
	entries := pane.visibleEntries()
	switch mouse.Button {
	case tea.MouseButtonWheelUp:
		pane.cursor -= 3
		pane.clamp(m.visibleRows())
	case tea.MouseButtonWheelDown:
		pane.cursor += 3
		pane.clamp(m.visibleRows())
	case tea.MouseButtonLeft, tea.MouseButtonRight:
		if mouse.Action != tea.MouseActionPress || mouse.Y < 3 || mouse.Y >= m.height-4 {
			return m, nil
		}
		row := pane.offset + mouse.Y - 3
		if row < 0 || row >= len(entries) {
			return m, nil
		}
		pane.cursor = row
		if mouse.Button == tea.MouseButtonRight || mouse.Ctrl {
			entry := entries[row]
			pane.selected[entry.Path] = !pane.selected[entry.Path]
			if !pane.selected[entry.Path] {
				delete(pane.selected, entry.Path)
			}
			return m, nil
		}
		now := time.Now()
		doubleClick := m.lastClickPane == index && m.lastClickRow == row && now.Sub(m.lastClickAt) < 500*time.Millisecond
		m.lastClickPane, m.lastClickRow, m.lastClickAt = index, row, now
		if doubleClick {
			return m, m.openCurrent(false)
		}
	}
	return m, nil
}

func (m *Model) handleMainKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	pane := m.currentPane()
	visible := m.visibleRows()
	switch key.String() {
	case "f10", "ctrl+c":
		m.close()
		return m, tea.Quit
	case "f1":
		m.modal, m.helpOffset = modalHelp, 0
	case "ctrl+p", "ctrl+shift+p":
		m.openCommandPalette()
	case "ctrl+s":
		return m, m.openSyncCenter()
	case "f3", "alt+f":
		return m, m.startFuzzyFinder()
	case "ctrl+f":
		pane.filterInput, pane.search = !pane.filterInput, ""
		if pane.filterInput {
			m.setStatus("Filter: type to narrow the listing · Esc clears", false)
		} else {
			m.setStatus("Filter input closed", false)
		}
	case "tab":
		m.focus = 1 - m.focus
		m.persistSessionState()
	case "ctrl+t":
		return m, m.newTab(m.focus)
	case "ctrl+w":
		return m, m.closeTab(m.focus)
	case "ctrl+right", "ctrl+pgdown", "alt+right", "f12":
		return m, m.changeTab(m.focus, 1)
	case "ctrl+left", "shift+tab", "ctrl+pgup", "alt+left", "f11":
		return m, m.changeTab(m.focus, -1)
	case "up":
		pane.cursor, pane.search = pane.cursor-1, ""
		pane.clamp(visible)
	case "down":
		pane.cursor, pane.search = pane.cursor+1, ""
		pane.clamp(visible)
	case "pgup":
		pane.cursor, pane.search = pane.cursor-visible, ""
		pane.clamp(visible)
	case "pgdown":
		pane.cursor, pane.search = pane.cursor+visible, ""
		pane.clamp(visible)
	case "home":
		pane.cursor, pane.search = 0, ""
		pane.clamp(visible)
	case "end":
		pane.cursor, pane.search = len(pane.visibleEntries())-1, ""
		pane.clamp(visible)
	case "esc":
		switch {
		case pane.filter != "":
			pane.clearInput()
			pane.cursor, pane.offset = 0, 0
			pane.clamp(visible)
			m.setStatus("Filter cleared", false)
		case pane.filterInput:
			pane.clearInput()
			m.setStatus("Filter input closed", false)
		case pane.search != "":
			pane.search = ""
			m.setStatus("Search cleared", false)
		}
	case " ":
		if entry, ok := pane.current(); ok {
			pane.selected[entry.Path] = !pane.selected[entry.Path]
			if !pane.selected[entry.Path] {
				delete(pane.selected, entry.Path)
			}
		}
	case "ctrl+a":
		entries := pane.visibleEntries()
		for _, entry := range entries {
			pane.selected[entry.Path] = true
		}
		m.setStatus(fmt.Sprintf("Selected all %d visible entries", len(entries)), false)
	case "*":
		selected := 0
		for _, entry := range pane.visibleEntries() {
			if pane.selected[entry.Path] {
				delete(pane.selected, entry.Path)
			} else {
				pane.selected[entry.Path] = true
				selected++
			}
		}
		m.setStatus(fmt.Sprintf("Inverted visible selection · %d selected", selected), false)
	case "enter":
		return m, m.openCurrent(false)
	case "f4", "ctrl+o":
		return m, m.openCurrent(true)
	case "f16": // Terminals encode Shift+F4 as the extended F16 key.
		if pane.location.Backend.ID() == "trash" {
			m.setStatus("Files cannot be created in the Recycle Bin", true)
			break
		}
		m.startPrompt("New file", "", promptCreateFile, false)
	case "backspace":
		if pane.filterInput && pane.filter != "" {
			runes := []rune(pane.filter)
			pane.filter, pane.cursor, pane.offset = string(runes[:len(runes)-1]), 0, 0
			pane.clamp(visible)
			if pane.filter == "" {
				m.setStatus("Filter cleared", false)
			}
			break
		}
		if pane.search != "" {
			runes := []rune(pane.search)
			pane.search = string(runes[:len(runes)-1])
			pane.jumpToSearch(visible)
			break
		}
		return m, m.openParent()
	case "ctrl+r":
		pane.loading = true
		return m, m.loadPaneCmd(m.focus)
	case "f2":
		if pane.location.Backend.ID() == "trash" {
			m.setStatus("Recycle Bin items cannot be renamed", true)
			break
		}
		if entry, ok := pane.current(); ok {
			m.startPrompt("Rename", entry.Name, promptRename, false)
		}
	case "f5":
		if pane.location.Backend.ID() == "trash" {
			if len(pane.chosen()) > 0 {
				m.modal, m.confirm = modalConfirm, confirmState{title: "Restore " + m.selectedSummary() + " to its original location?", action: confirmRestore}
			}
			break
		}
		if m.otherPane().location.Backend.ID() == "trash" {
			m.setStatus("Use F5 inside the Recycle Bin to restore items", true)
			break
		}
		chosen := pane.chosen()
		if len(chosen) > 0 {
			if len(chosen) == 1 {
				m.startTransferNamePrompt(false, chosen[0])
				break
			}
			if pane.location.Backend.ID() == m.otherPane().location.Backend.ID() && pane.location.Path == m.otherPane().location.Path {
				m.setStatus("Source and destination directories are the same", true)
				break
			}
			m.modal, m.confirm = modalConfirm, confirmState{title: "Copy " + m.selectedSummary() + " to the other pane?" + m.overwriteWarning(), action: confirmCopy}
		}
	case "f6":
		if pane.location.Backend.ID() == "trash" || m.otherPane().location.Backend.ID() == "trash" {
			m.setStatus("Items cannot be moved into or out of the Recycle Bin; use F5 there to restore", true)
			break
		}
		chosen := pane.chosen()
		if len(chosen) > 0 {
			if len(chosen) == 1 {
				m.startTransferNamePrompt(true, chosen[0])
				break
			}
			if pane.location.Backend.ID() == m.otherPane().location.Backend.ID() && pane.location.Path == m.otherPane().location.Path {
				m.setStatus("Source and destination directories are the same", true)
				break
			}
			m.modal, m.confirm = modalConfirm, confirmState{title: "Move " + m.selectedSummary() + " to the other pane?" + m.overwriteWarning(), action: confirmMove}
		}
	case "f7":
		if pane.location.Backend.ID() == "trash" {
			m.setStatus("Directories cannot be created in the Recycle Bin", true)
			break
		}
		m.startPrompt("New directory", "", promptMkdir, false)
	case "f8", "delete":
		if len(pane.chosen()) > 0 {
			switch pane.location.Backend.ID() {
			case "local":
				m.modal, m.confirm = modalConfirm, confirmState{title: "Move " + m.selectedSummary() + " to the Recycle Bin?", action: confirmTrash}
			case "trash":
				m.modal, m.confirm = modalConfirm, confirmState{title: "Permanently delete " + m.selectedSummary() + " from the Recycle Bin? This cannot be undone.", action: confirmDelete}
			default:
				m.modal, m.confirm = modalConfirm, confirmState{title: "Permanently delete remote " + m.selectedSummary() + "? This server has no portable Recycle Bin.", action: confirmDelete}
			}
		}
	case "alt+f5":
		chosen := pane.chosen()
		if _, ok := m.localPaths(chosen); !ok || m.otherPane().location.Backend.ID() != "local" {
			m.setStatus("Packing currently requires local source and destination panes", true)
			break
		}
		m.startPrompt("Archive path (.7z, .zip, .rar, .gz, .tar, .tar.gz)", filepath.Join(m.otherPane().location.Path, "archive.7z"), promptPack, false)
	case "alt+f6":
		entry, ok := pane.current()
		if !ok || entry.Dir || !archive.Supported(entry.Name) {
			m.setStatus("Select a supported archive", true)
			break
		}
		if pane.location.Backend.ID() != "local" || m.otherPane().location.Backend.ID() != "local" {
			m.setStatus("Extraction currently requires local source and destination panes", true)
			break
		}
		m.modal, m.confirm = modalConfirm, confirmState{title: "Extract " + entry.Name + " into the other pane?", action: confirmExtract}
	case "ctrl+n", "alt+c":
		m.modal, m.bookmarks = modalBookmarks, bookmarkState{}
	case "ctrl+]": // Kitty maps physical Ctrl+Shift+N to this distinct control byte.
		return m, m.disconnectNetwork()
	case "alt+r":
		return m, m.openTrash()
	case "ctrl+l":
		m.startPrompt("Location or connection URL", currentLocationString(*pane), promptLocation, false)
	case "ctrl+h":
		pane.showHidden = !pane.showHidden
		pane.cursor, pane.offset = 0, 0
		pane.clamp(visible)
		if pane.showHidden {
			m.setStatus("Hidden files shown", false)
		} else {
			m.setStatus("Hidden files hidden", false)
		}
		m.persistSessionState()
	case "ctrl+b":
		if pane.location.Backend.ID() == "trash" {
			m.setStatus("The Recycle Bin cannot be bookmarked", true)
			break
		}
		m.startPrompt("Bookmark name", pane.location.Backend.Label(), promptBookmarkName, false)
		m.prompt.checkboxVisible = pane.location.Backend.ID() != "local"
		m.prompt.checkboxChecked = pane.passwordSaved && pane.password != ""
		m.prompt.checkboxLabel = "Save password in config"
		m.prompt.checkboxHint = "Enter saves"
	case "alt+ctrl+c":
		if entry, ok := pane.current(); ok {
			if pane.location.Backend.ID() == "trash" && entry.OriginalPath != "" {
				return m, copyPathCmd(entry.OriginalPath)
			}
			return m, copyPathCmd(locationStringForPath(*pane, entry.Path))
		}
	case "alt+u":
		if dirty := m.firstDirtyRemote(); dirty != nil {
			return m, m.uploadRemote(dirty)
		}
	case "alt+m":
		leftPath, rightPath, ok := m.mergerPaths()
		if !ok {
			m.setStatus("Merger requires two selected local entries in the active pane, or one chosen local entry in each pane", true)
			break
		}
		return m, tea.ExecProcess(exec.Command("merger", leftPath, rightPath), func(err error) tea.Msg { return appClosedMsg{err: err} })
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9":
		keyName := key.String()
		tabIndex := int(keyName[len(keyName)-1] - '1')
		return m, m.activateTab(m.focus, tabIndex)
	case "alt+0":
		return m, m.activateTab(m.focus, -1)
	default:
		if key.Type == tea.KeyRunes && !key.Alt {
			var typed strings.Builder
			for _, character := range key.Runes {
				if unicode.IsPrint(character) && (!unicode.IsSpace(character) || key.Paste) {
					typed.WriteRune(character)
				}
			}
			if typed.Len() == 0 {
				break
			}
			if pane.filterInput {
				pane.filter += typed.String()
				pane.cursor, pane.offset = 0, 0
				pane.clamp(visible)
				break
			}
			pane.search += typed.String()
			if !pane.jumpToSearch(visible) {
				m.setStatus(fmt.Sprintf("No entry matches %q", pane.search), false)
			}
		}
	}
	return m, nil
}

func (m *Model) mergerPaths() (string, string, bool) {
	active := m.currentPane()
	if active.location.Backend.ID() == "local" {
		selected := active.selectedEntries()
		if len(selected) == 2 {
			return selected[0].Path, selected[1].Path, true
		}
	}
	if m.panes[0].location.Backend.ID() != "local" || m.panes[1].location.Backend.ID() != "local" {
		return "", "", false
	}
	left, right := m.panes[0].chosen(), m.panes[1].chosen()
	if len(left) != 1 || len(right) != 1 {
		return "", "", false
	}
	return left[0].Path, right[0].Path, true
}

func (m *Model) disconnectNetwork() tea.Cmd {
	target := m.currentPane().location.Backend
	if target.ID() == "local" || target.ID() == "trash" {
		m.setStatus("The active pane is not connected to a network", true)
		return nil
	}
	var commands []tea.Cmd
	replaced := 0
	for paneIndex := range m.tabs {
		for tabIndex, old := range m.tabs[paneIndex] {
			if old.location.Backend != target {
				continue
			}
			backend := vfs.NewLocal()
			returnPath := usableLocalDirectory(old.localReturn)
			replacement := &pane{
				location: vfs.Location{Backend: backend, Path: returnPath, Raw: returnPath},
				selected: make(map[string]bool), showHidden: old.showHidden,
				localReturn: returnPath, loading: true,
			}
			m.tabs[paneIndex][tabIndex] = replacement
			m.backends = append(m.backends, backend)
			replaced++
			if tabIndex == m.activeTab[paneIndex] {
				m.panes[paneIndex] = replacement
				commands = append(commands, m.loadPaneCmd(paneIndex))
			}
		}
	}
	for index, backend := range m.backends {
		if backend == target {
			m.backends = append(m.backends[:index], m.backends[index+1:]...)
			break
		}
	}
	_ = target.Close()
	if replaced == 1 {
		m.setStatus("Network connection disconnected", false)
	} else {
		m.setStatus(fmt.Sprintf("Network connection disconnected from %d tabs", replaced), false)
	}
	return tea.Batch(commands...)
}

func usableLocalDirectory(preferred string) string {
	for _, candidate := range []string{preferred, mustWorkingDirectory(), mustHomeDirectory(), "/"} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return filepath.Clean(candidate)
		}
	}
	return "/"
}

func mustWorkingDirectory() string {
	directory, _ := os.Getwd()
	return directory
}

func mustHomeDirectory() string {
	directory, _ := os.UserHomeDir()
	return directory
}

func (m *Model) handleModalKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal {
	case modalHelp:
		pageSize := m.helpPageSize()
		lastOffset := max(0, len(helpBindings)-pageSize)
		switch key.String() {
		case "esc", "f1":
			m.modal = modalNone
		case "up", "k":
			m.helpOffset = max(0, m.helpOffset-1)
		case "down", "j":
			m.helpOffset = min(lastOffset, m.helpOffset+1)
		case "pgup":
			m.helpOffset = max(0, m.helpOffset-pageSize)
		case "pgdown":
			m.helpOffset = min(lastOffset, m.helpOffset+pageSize)
		case "home":
			m.helpOffset = 0
		case "end":
			m.helpOffset = lastOffset
		}
	case modalPrompt:
		return m.handlePromptKey(key)
	case modalConfirm:
		switch key.String() {
		case "y", "enter":
			confirmation := m.confirm
			m.modal, m.confirm = modalNone, confirmState{}
			return m, m.runConfirmed(confirmation)
		case "n", "esc":
			if m.confirm.action == confirmTrustCertificate {
				m.pendingTrust = certificateTrustState{}
			}
			m.modal, m.confirm = modalNone, confirmState{}
		}
	case modalApplications:
		items := m.applications.items
		switch key.String() {
		case "up", "k":
			m.applications.cursor = max(0, m.applications.cursor-1)
		case "down", "j":
			m.applications.cursor = min(len(items)-1, m.applications.cursor+1)
		case "enter":
			return m, m.launchSelectedApplication(false)
		case "d":
			return m, m.launchSelectedApplication(true)
		case "esc":
			m.modal = modalNone
		}
	case modalBookmarks:
		switch key.String() {
		case "up", "k":
			m.bookmarks.cursor = max(0, m.bookmarks.cursor-1)
		case "down", "j":
			m.bookmarks.cursor = min(len(m.config.Bookmarks)-1, m.bookmarks.cursor+1)
		case "alt+up":
			m.moveBookmark(-1)
		case "alt+down":
			m.moveBookmark(1)
		case "s":
			m.sortBookmarks()
		case "a":
			m.startPrompt("FTP, FTPES/FTPS, SFTP or SMB URL", "", promptLocation, false)
		case "e":
			if m.bookmarks.cursor >= 0 && m.bookmarks.cursor < len(m.config.Bookmarks) {
				bookmark := m.config.Bookmarks[m.bookmarks.cursor]
				m.startPrompt("Connection display name", bookmark.Name, promptBookmarkDisplayName, false)
				m.prompt.pendingRaw = bookmark.Name
			}
		case "g":
			if m.bookmarks.cursor >= 0 && m.bookmarks.cursor < len(m.config.Bookmarks) {
				bookmark := m.config.Bookmarks[m.bookmarks.cursor]
				m.startPrompt("Bookmark group (empty = Ungrouped)", bookmark.Group, promptBookmarkGroup, false)
				m.prompt.pendingRaw = bookmark.Name
			}
		case "v":
			if m.bookmarks.cursor >= 0 && m.bookmarks.cursor < len(m.config.Bookmarks) {
				bookmark := m.config.Bookmarks[m.bookmarks.cursor]
				if config.IsLocalLocation(bookmark.Location) {
					m.setStatus("Local bookmarks do not need gopass credentials", false)
					break
				}
				m.startPrompt("gopass credential ID or unique title (empty = unlink)", bookmark.CredentialRef, promptBookmarkCredential, false)
				m.prompt.pendingRaw = bookmark.Name
			}
		case "d":
			if len(m.config.Bookmarks) > 0 {
				m.config.RemoveBookmark(m.bookmarks.cursor)
				if err := m.config.Save(); err != nil {
					m.setStatus("Save bookmarks: "+err.Error(), true)
				}
				m.bookmarks.cursor = min(m.bookmarks.cursor, max(0, len(m.config.Bookmarks)-1))
			}
		case "enter":
			if m.bookmarks.cursor >= 0 && m.bookmarks.cursor < len(m.config.Bookmarks) {
				bookmark := m.config.Bookmarks[m.bookmarks.cursor]
				if config.IsLocalLocation(bookmark.Location) {
					m.modal = modalNone
					m.busy, m.busyLabel = true, "Opening location"
					return m, tea.Batch(connectCmd(m.focus, bookmark.Location, "", false), busyTickCmd())
				}
				if bookmark.CredentialRef != "" {
					request := credentialRequest{
						index: m.focus, bookmarkName: bookmark.Name,
						location: bookmark.Location, ref: bookmark.CredentialRef,
					}
					if m.credentialMaster != "" && time.Now().Before(m.credentialUntil) {
						request.master = m.credentialMaster
						m.modal = modalNone
						m.busy, m.busyLabel = true, "Unlocking credential"
						return m, tea.Batch(resolveCredentialCmd(request), busyTickCmd())
					}
					m.pendingCredential = request
					m.startPrompt("gopass vault password", "", promptCredentialPassword, true)
					return m, nil
				}
				if bookmark.Password != "" {
					m.modal = modalNone
					m.busy, m.busyLabel = true, "Connecting"
					return m, tea.Batch(connectCmd(m.focus, bookmark.Location, bookmark.Password, true), busyTickCmd())
				}
				m.startPasswordPrompt(bookmark.Location)
			}
		case "esc", "c":
			m.modal = modalNone
		}
	case modalCommands:
		return m.updateCommandPalette(key)
	case modalSync:
		return m.updateSyncCenter(key)
	}
	return m, nil
}

func (m *Model) moveBookmark(delta int) {
	if m.bookmarks.cursor < 0 || m.bookmarks.cursor >= len(m.config.Bookmarks) {
		return
	}
	previous := append([]config.Bookmark(nil), m.config.Bookmarks...)
	oldIndex := m.bookmarks.cursor
	newIndex := m.config.MoveBookmark(oldIndex, delta)
	if newIndex == oldIndex {
		target := oldIndex + delta
		if target >= 0 && target < len(m.config.Bookmarks) &&
			!strings.EqualFold(m.config.Bookmarks[oldIndex].Group, m.config.Bookmarks[target].Group) {
			m.setStatus("Use g to move a bookmark to another group", false)
		}
		return
	}
	if err := m.config.Save(); err != nil {
		m.config.Bookmarks = previous
		m.setStatus("Save bookmark order: "+err.Error(), true)
		return
	}
	m.bookmarks.cursor = newIndex
	m.setStatus("Bookmark order saved", false)
}

func (m *Model) sortBookmarks() {
	if len(m.config.Bookmarks) == 0 {
		return
	}
	m.bookmarks.cursor = max(0, min(len(m.config.Bookmarks)-1, m.bookmarks.cursor))
	selectedName := m.config.Bookmarks[m.bookmarks.cursor].Name
	previous := append([]config.Bookmark(nil), m.config.Bookmarks...)
	m.config.SortBookmarks()
	if err := m.config.Save(); err != nil {
		m.config.Bookmarks = previous
		m.setStatus("Save bookmark order: "+err.Error(), true)
		return
	}
	for index, bookmark := range m.config.Bookmarks {
		if bookmark.Name == selectedName {
			m.bookmarks.cursor = index
			break
		}
	}
	m.setStatus("Bookmarks sorted by group, protocol, and name", false)
}

func (m *Model) handlePromptKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	prompt := &m.prompt
	keyName := promptControlKey(key.String())
	if prompt.checkboxVisible {
		switch keyName {
		case "tab", "shift+tab":
			prompt.checkboxFocus = !prompt.checkboxFocus
			return m, nil
		case " ":
			if prompt.checkboxFocus {
				prompt.checkboxChecked = !prompt.checkboxChecked
				return m, nil
			}
		}
		if prompt.checkboxFocus && keyName != "enter" && keyName != "esc" {
			return m, nil
		}
	}
	if editPromptText(prompt, keyName) {
		return m, nil
	}
	switch keyName {
	case "esc":
		if prompt.action == promptBookmarkDisplayName || prompt.action == promptBookmarkGroup ||
			prompt.action == promptBookmarkCredential || prompt.action == promptCredentialPassword {
			if prompt.action == promptCredentialPassword {
				m.pendingCredential = credentialRequest{}
			}
			m.modal, m.prompt = modalBookmarks, promptState{}
			return m, nil
		}
		m.modal, m.prompt = modalNone, promptState{}
	case "enter":
		value, action, pending, checked := string(prompt.value), prompt.action, prompt.pendingRaw, prompt.checkboxChecked
		m.modal, m.prompt = modalNone, promptState{}
		return m, m.submitPrompt(action, value, pending, checked)
	default:
		if key.Type == tea.KeyRunes && utf8.RuneCountInString(string(key.Runes)) > 0 {
			value := append([]rune(nil), prompt.value[:prompt.cursor]...)
			value = append(value, key.Runes...)
			value = append(value, prompt.value[prompt.cursor:]...)
			prompt.value = value
			prompt.cursor += len(key.Runes)
		}
	}
	return m, nil
}

func promptControlKey(key string) string {
	switch key {
	case "ctrl+h", "ctrl+w":
		// Most terminals encode Ctrl+Backspace as one of these control keys.
		return "ctrl+backspace"
	default:
		return key
	}
}

func promptUnknownControlKey(message tea.Msg) string {
	// Bubble Tea v1 exposes unrecognised modified-key CSI sequences only through
	// a private fmt.Stringer message. Cover the common xterm and Kitty encodings
	// until the dependency provides public Ctrl+Backspace/Ctrl+Delete key types.
	stringer, ok := message.(fmt.Stringer)
	if !ok {
		return ""
	}
	switch stringer.String() {
	case "?CSI[51 59 53 126]?", // CSI 3;5~ (xterm Ctrl+Delete)
		"?CSI[53 55 51 52 57 59 53 117]?": // CSI 57349;5u (Kitty Ctrl+Delete)
		return "ctrl+delete"
	case "?CSI[49 50 55 59 53 117]?", // CSI 127;5u (Kitty Ctrl+Backspace)
		"?CSI[53 55 51 52 55 59 53 117]?",    // CSI 57347;5u (Kitty Ctrl+Backspace)
		"?CSI[50 55 59 53 59 49 50 55 126]?": // CSI 27;5;127~ (modifyOtherKeys)
		return "ctrl+backspace"
	case "?CSI[53 55 51 53 48 59 53 117]?": // CSI 57350;5u (Kitty Ctrl+Left)
		return "ctrl+left"
	case "?CSI[53 55 51 53 49 59 53 117]?": // CSI 57351;5u (Kitty Ctrl+Right)
		return "ctrl+right"
	default:
		return ""
	}
}

func editPromptText(prompt *promptState, key string) bool {
	prompt.cursor = max(0, min(prompt.cursor, len(prompt.value)))
	switch key {
	case "left":
		prompt.cursor = max(0, prompt.cursor-1)
	case "right":
		prompt.cursor = min(len(prompt.value), prompt.cursor+1)
	case "ctrl+left":
		prompt.cursor = previousPromptWord(prompt.value, prompt.cursor)
	case "ctrl+right":
		prompt.cursor = nextPromptWord(prompt.value, prompt.cursor)
	case "home", "ctrl+a":
		prompt.cursor = 0
	case "end", "ctrl+e":
		prompt.cursor = len(prompt.value)
	case "backspace":
		if prompt.cursor > 0 {
			prompt.value = append(prompt.value[:prompt.cursor-1], prompt.value[prompt.cursor:]...)
			prompt.cursor--
		}
	case "delete":
		if prompt.cursor < len(prompt.value) {
			prompt.value = append(prompt.value[:prompt.cursor], prompt.value[prompt.cursor+1:]...)
		}
	case "ctrl+backspace":
		start := previousPromptWord(prompt.value, prompt.cursor)
		prompt.value = append(prompt.value[:start], prompt.value[prompt.cursor:]...)
		prompt.cursor = start
	case "ctrl+delete":
		end := nextPromptWordEnd(prompt.value, prompt.cursor)
		prompt.value = append(prompt.value[:prompt.cursor], prompt.value[end:]...)
	default:
		return false
	}
	return true
}

func previousPromptWord(value []rune, cursor int) int {
	cursor = max(0, min(cursor, len(value)))
	for cursor > 0 && !isPromptWordRune(value[cursor-1]) {
		cursor--
	}
	for cursor > 0 && isPromptWordRune(value[cursor-1]) {
		cursor--
	}
	return cursor
}

func nextPromptWord(value []rune, cursor int) int {
	cursor = max(0, min(cursor, len(value)))
	for cursor < len(value) && isPromptWordRune(value[cursor]) {
		cursor++
	}
	for cursor < len(value) && !isPromptWordRune(value[cursor]) {
		cursor++
	}
	return cursor
}

func nextPromptWordEnd(value []rune, cursor int) int {
	cursor = max(0, min(cursor, len(value)))
	for cursor < len(value) && !isPromptWordRune(value[cursor]) {
		cursor++
	}
	for cursor < len(value) && isPromptWordRune(value[cursor]) {
		cursor++
	}
	return cursor
}

func isPromptWordRune(character rune) bool {
	return character == '_' || unicode.IsLetter(character) || unicode.IsNumber(character) || unicode.IsMark(character)
}

func (m *Model) submitPrompt(action promptAction, value, pending string, optionChecked bool) tea.Cmd {
	value = strings.TrimSpace(value)
	pane := m.currentPane()
	switch action {
	case promptMkdir:
		if value == "" {
			return nil
		}
		backend, path := pane.location.Backend, pane.location.Backend.Join(pane.location.Path, value)
		return m.simpleOperation("Create directory", func(ctx context.Context) error { return backend.Mkdir(ctx, path, 0o755) })
	case promptCreateFile:
		if value == "" {
			return nil
		}
		backend, directory := pane.location.Backend, pane.location.Path
		if !validTransferName(backend, value) {
			m.setStatus("File name must be a single name", true)
			m.startPrompt("New file", value, promptCreateFile, false)
			return nil
		}
		return m.simpleOperation("Create file", func(ctx context.Context) error {
			return vfs.CreateEmpty(ctx, backend, directory, value, 0o644)
		})
	case promptRename:
		entry, ok := pane.current()
		if !ok || value == "" || value == entry.Name {
			return nil
		}
		backend, newPath := pane.location.Backend, pane.location.Backend.Join(pane.location.Path, value)
		return m.simpleOperation("Rename", func(ctx context.Context) error { return backend.Rename(ctx, entry.Path, newPath) })
	case promptCopyAs, promptMoveAs:
		entries := pane.chosen()
		if len(entries) != 1 {
			m.setStatus("Choose exactly one entry to give it a destination name", true)
			return nil
		}
		move := action == promptMoveAs
		if !validTransferName(m.otherPane().location.Backend, value) {
			m.setStatus("Destination name must be a single file or directory name", true)
			m.startTransferNamePrompt(move, entries[0])
			m.prompt.value, m.prompt.cursor = []rune(value), len([]rune(value))
			return nil
		}
		destination := m.otherPane()
		destinationPath := destination.location.Backend.Join(destination.location.Path, value)
		if pane.location.Backend.ID() == destination.location.Backend.ID() &&
			destination.location.Backend.Clean(entries[0].Path) == destination.location.Backend.Clean(destinationPath) {
			m.setStatus("Source and destination are the same; enter a different name", true)
			m.startTransferNamePrompt(move, entries[0])
			return nil
		}
		verb, confirmAction := "Copy", confirmCopy
		if move {
			verb, confirmAction = "Move", confirmMove
		}
		m.modal = modalConfirm
		m.confirm = confirmState{
			title:  verb + " " + entries[0].Name + " to the other pane as " + value + "?" + m.overwriteWarningFor([]string{value}),
			action: confirmAction, destinationName: value,
		}
	case promptLocation:
		if value == "" {
			return nil
		}
		normalized := vfs.NormalizeLocation(value)
		if strings.Contains(normalized, "://") && !strings.HasPrefix(normalized, "file://") {
			m.startRemoteDirectoryPrompt(normalized)
			return nil
		}
		m.busy, m.busyLabel = true, "Opening location"
		return tea.Batch(connectCmd(m.focus, normalized, "", false), busyTickCmd())
	case promptPassword:
		m.busy, m.busyLabel = true, "Connecting"
		return tea.Batch(connectCmd(m.focus, pending, value, false), busyTickCmd())
	case promptRemoteDirectory:
		location, err := remoteLocationWithDirectory(pending, value, optionChecked)
		if err != nil {
			m.setStatus("Remote directory: "+err.Error(), true)
			return nil
		}
		m.startPasswordPrompt(location)
		return nil
	case promptPack:
		inputs, ok := m.localPaths(pane.chosen())
		if !ok || value == "" {
			return nil
		}
		return m.simpleOperation("Create archive", func(ctx context.Context) error { return archive.Pack(ctx, inputs, value) })
	case promptBookmarkName:
		if value == "" {
			return nil
		}
		password := ""
		if optionChecked {
			password = pane.password
		}
		m.config.AddBookmark(config.Bookmark{Name: value, Location: currentLocationString(*pane), Password: password})
		if err := m.config.Save(); err != nil {
			m.setStatus("Save bookmark: "+err.Error(), true)
		} else {
			pane.passwordSaved = password != ""
			message := "Saved bookmark " + value
			if pane.passwordSaved {
				message += " with password"
			}
			m.setStatus(message, false)
		}
	case promptBookmarkDisplayName:
		previousBookmarks := append([]config.Bookmark(nil), m.config.Bookmarks...)
		if err := m.config.RenameBookmark(pending, value); err != nil {
			m.setStatus("Rename connection: "+err.Error(), true)
			m.startPrompt("Connection display name", value, promptBookmarkDisplayName, false)
			m.prompt.pendingRaw = pending
			return nil
		}
		selectedName := value
		if err := m.config.Save(); err != nil {
			m.config.Bookmarks = previousBookmarks
			selectedName = pending
			m.setStatus("Save connections: "+err.Error(), true)
		} else {
			m.setStatus("Connection name changed to "+value, false)
		}
		m.modal = modalBookmarks
		for index, bookmark := range m.config.Bookmarks {
			if strings.EqualFold(bookmark.Name, selectedName) {
				m.bookmarks.cursor = index
				break
			}
		}
	case promptBookmarkGroup:
		previousBookmarks := append([]config.Bookmark(nil), m.config.Bookmarks...)
		index := -1
		for bookmarkIndex, bookmark := range m.config.Bookmarks {
			if strings.EqualFold(bookmark.Name, pending) {
				index = bookmarkIndex
				break
			}
		}
		if index < 0 {
			m.setStatus("Bookmark no longer exists", true)
			m.modal = modalBookmarks
			return nil
		}
		m.bookmarks.cursor = m.config.SetBookmarkGroup(index, value)
		if err := m.config.Save(); err != nil {
			m.config.Bookmarks = previousBookmarks
			for bookmarkIndex, bookmark := range m.config.Bookmarks {
				if strings.EqualFold(bookmark.Name, pending) {
					m.bookmarks.cursor = bookmarkIndex
					break
				}
			}
			m.setStatus("Save bookmark group: "+err.Error(), true)
		} else if value == "" {
			m.setStatus("Moved "+pending+" to Ungrouped", false)
		} else {
			m.setStatus("Moved "+pending+" to group "+value, false)
		}
		m.modal = modalBookmarks
	case promptBookmarkCredential:
		previousBookmarks := append([]config.Bookmark(nil), m.config.Bookmarks...)
		index := -1
		for bookmarkIndex, bookmark := range m.config.Bookmarks {
			if strings.EqualFold(bookmark.Name, pending) {
				index = bookmarkIndex
				break
			}
		}
		if index < 0 {
			m.setStatus("Bookmark no longer exists", true)
			m.modal = modalBookmarks
			return nil
		}
		m.config.SetBookmarkCredential(index, value)
		if err := m.config.Save(); err != nil {
			m.config.Bookmarks = previousBookmarks
			m.setStatus("Save credential reference: "+err.Error(), true)
		} else if value == "" {
			m.setStatus("Removed gopass credential from "+pending, false)
		} else {
			m.setStatus("Linked "+pending+" to encrypted gopass credential", false)
		}
		m.bookmarks.cursor = index
		m.modal = modalBookmarks
	case promptCredentialPassword:
		if value == "" || m.pendingCredential.ref == "" {
			m.pendingCredential = credentialRequest{}
			m.modal = modalBookmarks
			m.setStatus("gopass vault password is required", true)
			return nil
		}
		request := m.pendingCredential
		request.master = value
		m.pendingCredential = credentialRequest{}
		m.busy, m.busyLabel = true, "Unlocking credential"
		return tea.Batch(resolveCredentialCmd(request), busyTickCmd())
	}
	return nil
}

func validTransferName(backend vfs.Backend, name string) bool {
	return name != "" && name != "." && name != ".." && backend.Base(name) == name
}

func (m *Model) runConfirmed(confirmation confirmState) tea.Cmd {
	source, destination := m.currentPane(), m.otherPane()
	entries := append([]vfs.Entry(nil), source.chosen()...)
	switch confirmation.action {
	case confirmCopy:
		if confirmation.destinationName != "" && len(entries) == 1 {
			return m.transferOperationAs("Copy", false, source.location.Backend, destination.location.Backend, destination.location.Path, confirmation.destinationName, entries[0])
		}
		return m.transferOperation("Copy", false, source.location.Backend, destination.location.Backend, destination.location.Path, entries)
	case confirmMove:
		if confirmation.destinationName != "" && len(entries) == 1 {
			return m.transferOperationAs("Move", true, source.location.Backend, destination.location.Backend, destination.location.Path, confirmation.destinationName, entries[0])
		}
		return m.transferOperation("Move", true, source.location.Backend, destination.location.Backend, destination.location.Path, entries)
	case confirmTrash:
		bin, err := trash.New()
		if err != nil {
			m.setStatus("Open Recycle Bin: "+err.Error(), true)
			return nil
		}
		return m.simpleOperation("Move to Recycle Bin", func(ctx context.Context) error {
			for _, entry := range entries {
				if err := bin.Put(ctx, entry.Path); err != nil {
					return err
				}
			}
			return nil
		})
	case confirmRestore:
		bin, ok := source.location.Backend.(*trash.Backend)
		if !ok {
			m.setStatus("The active pane is not the Recycle Bin", true)
			return nil
		}
		return m.simpleOperation("Restore from Recycle Bin", func(ctx context.Context) error {
			for _, entry := range entries {
				if err := bin.Restore(ctx, entry.Path); err != nil {
					return err
				}
			}
			return nil
		})
	case confirmDelete:
		backend := source.location.Backend
		return m.simpleOperation("Delete", func(ctx context.Context) error {
			for _, entry := range entries {
				if err := backend.Remove(ctx, entry.Path, entry.Dir); err != nil {
					return err
				}
			}
			return nil
		})
	case confirmExtract:
		entry, ok := source.current()
		if !ok {
			return nil
		}
		destinationPath := destination.location.Path
		return m.simpleOperation("Extract archive", func(ctx context.Context) error { return archive.Extract(ctx, entry.Path, destinationPath) })
	case confirmTrustCertificate:
		pending := m.pendingTrust
		m.pendingTrust = certificateTrustState{}
		location, err := locationWithCertificatePin(pending.raw, pending.fingerprint)
		if err != nil {
			m.setStatus("Trust certificate: "+err.Error(), true)
			return nil
		}
		m.busy, m.busyLabel = true, "Reconnecting with pinned certificate"
		return tea.Batch(connectCmd(pending.index, location, pending.password, pending.passwordSaved), busyTickCmd())
	}
	return nil
}

func (m *Model) simpleOperation(description string, operation func(context.Context) error) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	tick := m.startBusy(description, cancel)
	command := func() tea.Msg { return operationDoneMsg{description: description, err: operation(ctx)} }
	return tea.Batch(command, tick)
}

func (m *Model) transferOperation(description string, move bool, source, destination vfs.Backend, destinationPath string, entries []vfs.Entry) tea.Cmd {
	return m.transferOperationWithName(description, move, source, destination, destinationPath, "", entries)
}

func (m *Model) transferOperationAs(description string, move bool, source, destination vfs.Backend, destinationPath, destinationName string, entry vfs.Entry) tea.Cmd {
	return m.transferOperationWithName(description, move, source, destination, destinationPath, destinationName, []vfs.Entry{entry})
}

func (m *Model) transferOperationWithName(description string, move bool, source, destination vfs.Backend, destinationPath, destinationName string, entries []vfs.Entry) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	tick := m.startBusy(description, cancel)
	progressChannel := m.progressCh
	command := func() tea.Msg {
		for _, entry := range entries {
			progress := func(value vfs.TransferProgress) {
				select {
				case progressChannel <- value:
				default:
				}
			}
			var err error
			if move && destinationName != "" {
				err = vfs.MoveAs(ctx, source, entry, destination, destinationPath, destinationName, progress)
			} else if destinationName != "" {
				err = vfs.CopyAs(ctx, source, entry, destination, destinationPath, destinationName, progress)
			} else if move {
				err = vfs.Move(ctx, source, entry, destination, destinationPath, progress)
			} else {
				err = vfs.Copy(ctx, source, entry, destination, destinationPath, progress)
			}
			if err != nil {
				return operationDoneMsg{description: description, err: err}
			}
		}
		return operationDoneMsg{description: description}
	}
	return tea.Batch(command, tick)
}

func (m *Model) stopBusy() {
	if m.cancel != nil {
		m.cancel()
	}
	m.busy, m.busyLabel, m.cancel, m.progressCh = false, "", nil, nil
}

// openParent moves the active pane up one level and keeps the highlight on the
// directory that was just left.
func (m *Model) openParent() tea.Cmd {
	pane := m.currentPane()
	current := pane.location.Path
	parent := pane.location.Backend.Dir(current)
	if parent == current {
		return nil
	}
	pane.enterDirectory(parent)
	pane.revealPath = current
	return m.loadPaneCmd(m.focus)
}

func (m *Model) openCurrent(chooser bool) tea.Cmd {
	pane := m.currentPane()
	entry, ok := pane.current()
	if !ok {
		return nil
	}
	if entry.Dir {
		if pane.location.Backend.ID() == "trash" {
			m.setStatus("Recycle Bin directories are restored as complete items with F5", false)
			return nil
		}
		pane.enterDirectory(entry.Path)
		return m.loadPaneCmd(m.focus)
	}
	if pane.location.Backend.ID() == "local" {
		if chooser {
			m.busy, m.busyLabel = true, "Finding compatible applications"
			return tea.Batch(applicationsCmd(entry.Path, nil), busyTickCmd())
		}
		return m.launchDefault(entry.Path, nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	tick := m.startBusy("Downloading "+entry.Name+" for opening", cancel)
	backend, localPath := pane.location.Backend, m.tempPath(entry)
	command := func() tea.Msg {
		file, err := os.Create(localPath)
		if err != nil {
			return remoteDownloadedMsg{err: err}
		}
		err = backend.Download(ctx, entry.Path, file, nil)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(localPath)
			return remoteDownloadedMsg{err: err}
		}
		info, err := os.Stat(localPath)
		if err != nil {
			return remoteDownloadedMsg{err: err}
		}
		if err := vfs.ValidateDownloadedSize(entry.Size, info.Size()); err != nil {
			_ = os.Remove(localPath)
			return remoteDownloadedMsg{err: err}
		}
		remote := &remoteFile{
			localPath: localPath, backend: backend, remotePath: entry.Path, mode: entry.Mode,
			stamp: info.ModTime(), size: info.Size(), readOnly: backend.ID() == "trash",
		}
		return remoteDownloadedMsg{file: remote, chooser: chooser}
	}
	return tea.Batch(command, tick)
}

func (m *Model) launchDefault(path string, _ *remoteFile) tea.Cmd {
	return runNonInteractiveProcess(openwith.DefaultLaunchCommand(path))
}

func (m *Model) launchSelectedApplication(setDefault bool) tea.Cmd {
	state := m.applications
	if state.cursor < 0 || state.cursor >= len(state.items) {
		return nil
	}
	application := state.items[state.cursor]
	if setDefault {
		if err := openwith.SetDefault(application, state.mimeType); err != nil {
			m.setStatus(err.Error(), true)
			return nil
		}
		m.setStatus(application.Name+" is now the default for "+state.mimeType, false)
	}
	m.modal = modalNone
	return runNonInteractiveProcess(openwith.LaunchCommand(application, state.path))
}

// runNonInteractiveProcess keeps GUI launchers away from the terminal used by
// Bubble Tea. tea.ExecProcess is intended for interactive terminal programs:
// it releases the alternate screen and connects the child to the TUI's
// standard streams. GUI applications launched through gio can inherit those
// streams and print diagnostics after the TUI resumes, scrolling and
// corrupting the rendered frame.
func runNonInteractiveProcess(command *exec.Cmd) tea.Cmd {
	return func() tea.Msg {
		command.Stdin = nil
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		return appClosedMsg{err: command.Run()}
	}
}

func (m *Model) firstDirtyRemote() *remoteFile {
	for _, file := range m.remoteFiles {
		if file.dirty {
			return file
		}
	}
	return nil
}

func (m *Model) uploadRemote(file *remoteFile) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	tick := m.startBusy("Uploading edited remote file", cancel)
	command := func() tea.Msg {
		source, err := os.Open(file.localPath)
		if err != nil {
			return remoteUploadedMsg{file: file, err: err}
		}
		defer source.Close()
		err = file.backend.Upload(ctx, file.remotePath, source, file.mode, nil)
		return remoteUploadedMsg{file: file, err: err}
	}
	return tea.Batch(command, tick)
}
