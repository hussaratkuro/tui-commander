package ui

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"tui-commander/internal/theme"
	"tui-commander/internal/vfs"
)

var (
	colorBase    = lipgloss.Color("#1e1e2e")
	colorMantle  = lipgloss.Color("#181825")
	colorSurface = lipgloss.Color("#313244")
	colorOverlay = lipgloss.Color("#6c7086")
	colorText    = lipgloss.Color("#cdd6f4")
	colorSubtext = lipgloss.Color("#a6adc8")
	colorBlue    = lipgloss.Color("#89b4fa")
	colorGreen   = lipgloss.Color("#a6e3a1")
	colorYellow  = lipgloss.Color("#f9e2af")
	colorRed     = lipgloss.Color("#f38ba8")
	colorMauve   = lipgloss.Color("#cba6f7")

	baseStyle      lipgloss.Style
	pathStyle      lipgloss.Style
	focusedPath    lipgloss.Style
	selectedStyle  lipgloss.Style
	cursorStyle    lipgloss.Style
	directoryStyle lipgloss.Style
	mutedStyle     lipgloss.Style
	errorStyle     lipgloss.Style
	statusStyle    lipgloss.Style
	keyStyle       lipgloss.Style
	modalStyle     lipgloss.Style
	frameStyle     lipgloss.Style
	terminalHeader lipgloss.Style
)

func init() { applyTheme(theme.Current()) }

func applyTheme(p theme.Palette) {
	colorBase, colorMantle, colorSurface = p.Base, p.Mantle, p.Surface0
	colorOverlay, colorText, colorSubtext = p.Overlay0, p.Text, p.Subtext0
	colorBlue, colorGreen, colorYellow = p.Blue, p.Green, p.Yellow
	colorRed, colorMauve = p.Red, p.Mauve

	baseStyle = lipgloss.NewStyle().Background(colorBase).Foreground(colorText)
	pathStyle = lipgloss.NewStyle().Foreground(colorSubtext).Background(colorSurface)
	focusedPath = lipgloss.NewStyle().Bold(true).Foreground(p.OnAccent).Background(colorMauve)
	selectedStyle = lipgloss.NewStyle().Foreground(colorGreen).Background(colorBase)
	cursorStyle = lipgloss.NewStyle().Foreground(p.OnAccent).Background(colorMauve)
	directoryStyle = lipgloss.NewStyle().Bold(true).Foreground(colorBlue).Background(colorBase)
	mutedStyle = lipgloss.NewStyle().Foreground(colorOverlay).Background(colorBase)
	errorStyle = lipgloss.NewStyle().Foreground(colorRed).Background(colorMantle)
	statusStyle = lipgloss.NewStyle().Foreground(colorText).Background(colorMantle)
	keyStyle = lipgloss.NewStyle().Bold(true).Foreground(colorYellow).Background(colorMantle)
	modalStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorMauve).Padding(1, 2).Background(colorMantle).Foreground(colorText)
	frameStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(p.Surface1).Background(colorBase)
	terminalHeader = lipgloss.NewStyle().Bold(true).Foreground(colorMauve).Background(colorSurface)
}

func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "Starting tui-commander…"
	}
	contentWidth := max(1, m.width-2)
	contentHeight := max(1, m.height-2)
	footer := m.renderFooter(contentWidth)
	bodyHeight := max(1, contentHeight-lipgloss.Height(footer))
	var body string
	if m.modal != modalNone {
		content := m.renderModal(contentWidth, bodyHeight)
		body = lipgloss.Place(contentWidth, bodyHeight, lipgloss.Center, lipgloss.Center, content,
			lipgloss.WithWhitespaceBackground(colorBase))
	} else {
		paneAreaHeight := max(1, bodyHeight-1)
		terminalHeight := m.terminalPanelHeight()
		fileHeight := max(1, paneAreaHeight-terminalHeight)
		leftWidth := max(20, (contentWidth-1)/2)
		rightWidth := max(20, contentWidth-leftWidth-1)
		left := m.renderPane(0, leftWidth, fileHeight)
		right := m.renderPane(1, rightWidth, fileHeight)
		separator := lipgloss.NewStyle().Foreground(colorSurface).Background(colorBase).Height(fileHeight).Render(strings.Repeat("│\n", max(0, fileHeight-1)) + "│")
		body = m.renderLocationHeader(contentWidth) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, separator, right)
		if terminalHeight > 0 {
			body += "\n" + m.renderTerminal(contentWidth, terminalHeight)
		}
	}
	content := body + "\n" + footer
	return frameStyle.Width(contentWidth).Height(contentHeight).Render(content)
}

func (m *Model) renderPane(index, width, height int) string {
	pane := m.panes[index]
	entries := pane.visibleEntries()
	lines := []string{m.renderTabs(index, width)}
	rowCount := max(1, height-2)
	if pane.loading {
		lines = append(lines, mutedStyle.Width(width).Render("  Reading directory…"))
	} else if pane.err != nil {
		lines = append(lines, errorStyle.Width(width).Render("  "+truncateEnd(pane.err.Error(), width-2)))
	} else if len(entries) == 0 {
		message := "Empty directory"
		if pane.filter != "" {
			message = "No entries match the current filter"
		} else if len(pane.entries) > 0 {
			message = "No visible entries · Ctrl+H shows hidden files"
		}
		lines = append(lines, mutedStyle.Width(width).Render("  "+message))
	} else {
		pane.clamp(rowCount)
		for row := 0; row < rowCount; row++ {
			entryIndex := pane.offset + row
			if entryIndex >= len(entries) {
				lines = append(lines, baseStyle.Width(width).Render(""))
				continue
			}
			entry := entries[entryIndex]
			marker := "  "
			if pane.selected[entry.Path] {
				marker = "◆ "
			}
			icon := "·"
			if entry.Dir {
				icon = "▸"
			} else if entry.Link {
				icon = "↗"
			}
			// Marker, icon, size, spacing, and timestamp occupy 32 cells.
			// Reserving all 32 keeps the complete timestamp visible.
			nameWidth := max(6, width-32)
			displayName := entry.Name
			if pane.location.Backend.ID() == "trash" && entry.OriginalPath != "" {
				displayName += " ← " + filepath.Dir(entry.OriginalPath)
			}
			name := truncateEnd(displayName, nameWidth)
			size := vfs.HumanSize(entry.Size)
			if entry.Dir {
				size = "<DIR>"
			}
			date := entry.ModTime.Format("2006-01-02 15:04")
			line := fmt.Sprintf("%s%s %-*s %9s  %s", marker, icon, nameWidth, name, size, date)
			style := baseStyle
			if entry.Dir {
				style = directoryStyle
			}
			if pane.selected[entry.Path] {
				style = selectedStyle
			}
			if entryIndex == pane.cursor && index == m.focus {
				style = cursorStyle
			}
			lines = append(lines, style.Width(width).Render(truncateEnd(line, width)))
		}
	}
	for len(lines) < height {
		lines = append(lines, baseStyle.Width(width).Render(""))
	}
	count := fmt.Sprintf(" %d entries · %d selected ", len(entries), len(pane.selected))
	if len(entries) != len(pane.entries) {
		count = fmt.Sprintf(" %d/%d entries · %d selected ", len(entries), len(pane.entries), len(pane.selected))
	}
	lines = append(lines[:max(1, height-1)], mutedStyle.Width(width).Render(count))
	return strings.Join(lines, "\n")
}

func (m *Model) renderLocationHeader(width int) string {
	pane := m.currentPane()
	side := "Left"
	if m.focus == 1 {
		side = "Right"
	}
	label := fmt.Sprintf(" %s · %s  %s ", side, pane.location.Backend.Label(), pane.location.Path)
	git := pane.git.String()
	if git == "" {
		return focusedPath.Width(width).Render(truncateMiddle(label, width))
	}
	git = " " + git + " "
	label = truncateMiddle(label, max(1, width-lipgloss.Width(git)-1))
	gap := strings.Repeat(" ", max(1, width-lipgloss.Width(label)-lipgloss.Width(git)))
	return focusedPath.Width(width).Render(truncateEnd(label+gap+git, width))
}

func (m *Model) renderTabs(index, width int) string {
	var labels []string
	used := 1
	for tabIndex, tab := range m.tabs[index] {
		label := paneTabLabel(tabIndex, tab)
		if used+lipgloss.Width(label) > width {
			labels = append(labels, mutedStyle.Render(" … "))
			break
		}
		style := mutedStyle
		if tabIndex == m.activeTab[index] {
			style = lipgloss.NewStyle().Bold(true).Foreground(colorBase).Background(colorMauve)
		}
		labels = append(labels, style.Render(label))
		used += lipgloss.Width(label)
	}
	return baseStyle.Width(width).Render(" " + strings.Join(labels, ""))
}

func paneTabLabel(index int, tab *pane) string {
	name := tab.location.Backend.Base(tab.location.Path)
	if name == "." || name == "/" || name == "" {
		name = tab.location.Backend.Label()
	}
	return fmt.Sprintf(" %d:%s ", index+1, truncateEnd(name, 14))
}

func (m *Model) tabAt(paneIndex, paneWidth, x int) int {
	used := 1
	for tabIndex, tab := range m.tabs[paneIndex] {
		labelWidth := lipgloss.Width(paneTabLabel(tabIndex, tab))
		if used+labelWidth > paneWidth {
			return -1
		}
		if x >= used && x < used+labelWidth {
			return tabIndex
		}
		used += labelWidth
	}
	return -1
}

func (m *Model) renderFooter(width int) string {
	status := m.status
	style := statusStyle
	if m.statusError {
		style = errorStyle
	}
	if m.busy {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		status = frames[m.busyFrame%len(frames)] + " " + m.busyLabel
		if m.progress.Name != "" {
			status += " · " + m.progress.Name + " · " + vfs.HumanSize(m.progress.Bytes)
			if m.progress.Total > 0 {
				status += fmt.Sprintf("/%s (%d%%)", vfs.HumanSize(m.progress.Total), min(100, int(m.progress.Bytes*100/m.progress.Total)))
			}
		}
		status += " · Esc cancels"
	}
	if pane := m.currentPane(); !m.busy && !m.terminalVisible {
		switch {
		case pane.filter != "":
			status = fmt.Sprintf("Filter: %q · %d/%d matches · Backspace edits · Esc clears", pane.filter, len(pane.visibleEntries()), len(pane.entries))
			style = statusStyle
		case pane.filterInput:
			status = "Filter: type to narrow the listing · Esc clears"
			style = statusStyle
		case pane.search != "":
			status = fmt.Sprintf("Search: %q · Backspace edits · Esc clears", pane.search)
			style = statusStyle
		}
	}
	statusLine := style.Width(width).Render(" " + truncateEnd(status, max(1, width-2)))
	lines := []string{statusLine}
	for _, row := range shortcutRows(width) {
		lines = append(lines, statusStyle.Width(width).Render(" "+row))
	}
	return strings.Join(lines, "\n")
}

type shortcutHint struct{ key, action string }

var footerShortcuts = []shortcutHint{
	{"F1", "Help"}, {"F2", "Rename"}, {"F3", "Fuzzy"}, {"F4", "OpenWith"},
	{"F5", "Copy/Restore"}, {"F6", "Move"}, {"F7", "Mkdir"}, {"F8", "Trash/Delete"},
	{"F9", "Terminal"}, {"F10", "Quit"}, {"F11", "PrevTab"}, {"F12", "NextTab"},
	{"Ctrl+N", "Connections"}, {"Ctrl+L", "Location"}, {"Ctrl+B", "Bookmark"},
	{"Ctrl+R", "Refresh"}, {"Alt+F5", "Pack"}, {"Alt+F6", "Extract"},
	{"Alt+R", "RecycleBin"},
}

func shortcutRows(width int) []string {
	available := max(1, width-2)
	var rows []string
	current := ""
	for _, shortcut := range footerShortcuts {
		hint := keyStyle.Render(shortcut.key) + " " + shortcut.action
		candidate := hint
		if current != "" {
			candidate = current + "  " + hint
		}
		if current != "" && lipgloss.Width(candidate) > available {
			rows = append(rows, current)
			current = hint
		} else {
			current = candidate
		}
	}
	if current != "" {
		rows = append(rows, current)
	}
	return rows
}

func (m *Model) renderModal(width, height int) string {
	modalWidth := min(max(72, width*4/5), max(20, width-4))
	switch m.modal {
	case modalHelp:
		return m.renderHelp(modalWidth, height)
	case modalPrompt:
		return renderPromptModal(m.prompt, modalWidth)
	case modalConfirm:
		return modalStyle.Width(modalWidth).Render(m.confirm.title + "\n\ny / Enter confirms · n / Esc cancels")
	case modalApplications:
		var rows []string
		rows = append(rows, "Open with…", mutedStyle.Render(m.applications.mimeType), "")
		limit := min(len(m.applications.items), max(4, height-12))
		start := max(0, min(m.applications.cursor-limit/2, len(m.applications.items)-limit))
		for index := start; index < start+limit; index++ {
			application := m.applications.items[index]
			marker := "  "
			if application.Default {
				marker = "★ "
			}
			line := marker + application.Name
			if index == m.applications.cursor {
				line = cursorStyle.Width(modalWidth - 6).Render(line)
			}
			rows = append(rows, line)
		}
		rows = append(rows, "", "Enter opens once · d sets default and opens · Esc cancels")
		return modalStyle.Width(modalWidth).Render(strings.Join(rows, "\n"))
	case modalBookmarks:
		var rows []string
		rows = append(rows, "Bookmarks and connections", "")
		if len(m.config.Bookmarks) == 0 {
			rows = append(rows, mutedStyle.Render("No saved connections"))
		}
		lastGroup := "\x00"
		for index, bookmark := range m.config.Bookmarks {
			group := bookmark.Group
			if group == "" {
				group = "Ungrouped"
			}
			if lastGroup == "\x00" || !strings.EqualFold(lastGroup, group) {
				rows = append(rows, mutedStyle.Render("▾ "+group))
				lastGroup = group
			}
			line := fmt.Sprintf("  %-12s %s", connectionProtocol(bookmark.Location), bookmark.Name)
			line = truncateEnd(line, modalWidth-6)
			if index == m.bookmarks.cursor {
				line = cursorStyle.Width(modalWidth - 6).Render(line)
			}
			rows = append(rows, line)
		}
		rows = append(rows, "",
			"Enter opens · a adds · e renames · g group · v gopass · d deletes",
			"Alt+↑/↓ reorders in group · s sorts all · Esc closes")
		return modalStyle.Width(modalWidth).Render(strings.Join(rows, "\n"))
	case modalCommands:
		return m.renderCommandPalette(modalWidth, height)
	case modalSync:
		return m.renderSyncCenter(modalWidth, height)
	}
	return ""
}

type helpBinding struct{ keys, action string }

var helpBindings = []helpBinding{
	{"F1", "Open or close this Help menu"},
	{"F2", "Rename the highlighted entry"},
	{"F3 / Alt+F", "Open the fuzzy finder"},
	{"F4 / Ctrl+O", "Choose file application"},
	{"F5", "Copy; name one chosen entry"},
	{"F6", "Move; name one chosen entry"},
	{"F7", "Create a directory"},
	{"F8 / Delete", "Trash local / delete remote"},
	{"F9", "Toggle integrated terminal"},
	{"F10 / Ctrl+C", "Quit tui-commander"},
	{"F11 / Shift+Tab / Ctrl+Left / Ctrl+PgUp", "Activate the previous tab"},
	{"F12 / Ctrl+Right / Ctrl+PgDown", "Activate the next tab"},
	{"Click a tab", "Activate the clicked tab"},
	{"Tab", "Switch the active pane"},
	{"Ctrl+T", "Create tab in active pane"},
	{"Ctrl+W", "Close active tab"},
	{"Alt+1 … Alt+9", "Activate a numbered tab"},
	{"Alt+0", "Activate the last tab"},
	{"Up", "Move the highlight up one row"},
	{"Down", "Move highlight down one row"},
	{"PgUp", "Move the highlight up one page"},
	{"PgDown", "Move highlight down one page"},
	{"Home", "Jump highlight to first entry"},
	{"End", "Jump highlight to last entry"},
	{"Enter", "Open the highlighted entry"},
	{"Space", "Toggle highlighted selection"},
	{"Ctrl+A", "Select all visible entries"},
	{"*", "Invert visible selection"},
	{"Printable characters", "Jump highlight to typed name"},
	{"Ctrl+F", "Toggle quick filter input"},
	{"Backspace (filter/search)", "Remove last typed character"},
	{"Backspace (otherwise)", "Open parent, keep highlight"},
	{"Esc (filter/search)", "Clear the quick filter/search"},
	{"Ctrl+Numpad+ / Ctrl+Numpad-", "Zoom terminal font (kitty)"},
	{"Ctrl+H", "Toggle hidden files"},
	{"Ctrl+Shift+P / Ctrl+P", "Open fuzzy command palette"},
	{"Ctrl+S", "Open directory sync center"},
	{"Ctrl+N / Alt+C", "Open connection bookmarks"},
	{"Ctrl+Shift+N", "Disconnect active network"},
	{"Ctrl+L", "Enter path or connection URL"},
	{"Ctrl+B", "Bookmark active location"},
	{"Ctrl+Alt+C", "Copy highlighted full path"},
	{"Ctrl+R", "Refresh pane and Git status"},
	{"Alt+F5", "Create archive from selection"},
	{"Alt+F6", "Extract highlighted archive"},
	{"Alt+M", "Merge two chosen entries"},
	{"Alt+R", "Open or close Recycle Bin"},
	{"Alt+U", "Upload a changed remote file"},
	{"Up / k (menu)", "Select the previous menu item"},
	{"Down / j (menu)", "Select the next menu item"},
	{"Enter (menu)", "Activate selected menu item"},
	{"d (Open With)", "Set default app and open"},
	{"a (Connections)", "Add a connection URL"},
	{"e (Connections)", "Edit connection display name"},
	{"g (Bookmarks)", "Set highlighted bookmark group"},
	{"v (Bookmarks)", "Link gopass credential"},
	{"d (Connections)", "Delete highlighted bookmark"},
	{"Alt+Up / Alt+Down (Bookmarks)", "Move highlighted bookmark"},
	{"s (Bookmarks)", "Sort group/protocol/name"},
	{"Esc / c (Connections)", "Close connection bookmarks"},
	{"Left (prompt)", "Move the text cursor left"},
	{"Right (prompt)", "Move the text cursor right"},
	{"Ctrl+Left / Ctrl+Right (prompt)", "Move text cursor by one word"},
	{"Home / Ctrl+A (prompt)", "Move text cursor to start"},
	{"End / Ctrl+E (prompt)", "Move text cursor to end"},
	{"Backspace (prompt)", "Delete the previous character"},
	{"Delete (prompt)", "Delete the next character"},
	{"Ctrl+Backspace / Ctrl+Delete (prompt)", "Delete one word"},
	{"Enter (prompt)", "Submit the prompt"},
	{"Esc (prompt)", "Cancel the prompt"},
	{"Tab (option prompt)", "Focus the prompt checkbox"},
	{"Space (option prompt)", "Toggle the prompt checkbox"},
	{"y / Enter (confirmation)", "Confirm the operation"},
	{"n / Esc (confirmation)", "Cancel the operation"},
	{"Esc (busy operation)", "Request operation cancellation"},
	{"Up / k (Help)", "Scroll Help up one row"},
	{"Down / j (Help)", "Scroll Help down one row"},
	{"PgUp (Help)", "Scroll Help up one page"},
	{"PgDown (Help)", "Scroll Help down one page"},
	{"Home (Help)", "Jump to the start of Help"},
	{"End (Help)", "Jump to the end of Help"},
}

func connectionProtocol(location string) string {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme == "" {
		return "LOCAL"
	}
	return strings.ToUpper(parsed.Scheme)
}

func (m *Model) helpPageSize() int {
	footerHeight := 1 + len(shortcutRows(max(1, m.width-2)))
	bodyHeight := max(1, m.height-2-footerHeight)
	return helpPageSize(bodyHeight)
}

func helpPageSize(height int) int { return max(1, height-6) }

func (m *Model) renderHelp(width, height int) string {
	pageSize := min(len(helpBindings), helpPageSize(height))
	lastOffset := max(0, len(helpBindings)-pageSize)
	m.helpOffset = max(0, min(m.helpOffset, lastOffset))
	visible := helpBindings[m.helpOffset:min(len(helpBindings), m.helpOffset+pageSize)]
	keyWidth := min(34, max(18, width/2-2))
	actionWidth := max(1, width-keyWidth-8)
	lines := []string{"Keyboard shortcuts · one action per row"}
	for _, binding := range visible {
		keys := keyStyle.Width(keyWidth).Render(truncateEnd(binding.keys, keyWidth))
		lines = append(lines, keys+"  "+truncateEnd(binding.action, actionWidth))
	}
	position := fmt.Sprintf("Rows %d–%d of %d", m.helpOffset+1, m.helpOffset+len(visible), len(helpBindings))
	lines = append(lines, position+" · ↑/↓ or PgUp/PgDown scroll · F1/Esc closes")
	return modalStyle.Width(width).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderTerminal(width, height int) string {
	title := " Terminal"
	if m.terminal != nil {
		title += " · " + m.terminal.shell + " · " + m.terminal.cwd
		if m.terminal.exited {
			title += " · exited"
		}
	}
	title += " · F9 files "
	header := terminalHeader.Width(width).Render(truncateMiddle(title, width))
	viewportHeight := max(1, height-1)
	screen := baseStyle.Width(width).Height(viewportHeight).Render(renderTerminalScreen(m.terminal))
	return header + "\n" + screen
}

func renderInput(prompt promptState, width int) string {
	return renderInputWithFocus(prompt, width, true)
}

func renderInputWithFocus(prompt promptState, width int, focused bool) string {
	runes := append([]rune(nil), prompt.value...)
	if prompt.masked {
		for index := range runes {
			runes[index] = '•'
		}
	}
	cursor := max(0, min(prompt.cursor, len(runes)))
	before, current, after := string(runes[:cursor]), " ", ""
	if cursor < len(runes) {
		current = string(runes[cursor])
		after = string(runes[cursor+1:])
	}
	plain := before + current + after
	if utf8.RuneCountInString(plain) > width {
		start := max(0, cursor-width+4)
		visible := runes[start:min(len(runes), start+width-1)]
		cursor -= start
		before = string(visible[:min(cursor, len(visible))])
		current, after = " ", ""
		if cursor < len(visible) {
			current = string(visible[cursor])
			after = string(visible[cursor+1:])
		}
	}
	cursorStyle := lipgloss.NewStyle().Reverse(focused)
	return baseStyle.Width(width).Render(before + cursorStyle.Render(current) + after)
}

func renderPromptModal(prompt promptState, width int) string {
	lines := []string{prompt.title, "", renderInputWithFocus(prompt, width-6, !prompt.checkboxFocus)}
	if prompt.checkboxVisible {
		mark := " "
		if prompt.checkboxChecked {
			mark = "x"
		}
		label := prompt.checkboxLabel
		if label == "" {
			label = "Enable option"
		}
		checkbox := "[" + mark + "] " + label
		style := mutedStyle
		if prompt.checkboxFocus {
			style = cursorStyle
		}
		hint := prompt.checkboxHint
		if hint == "" {
			hint = "Enter confirms"
		}
		lines = append(lines, "", style.Render(checkbox), "", "Tab changes focus · Space toggles · "+hint+" · Esc cancels")
	} else {
		lines = append(lines, "", "Enter confirms · Esc cancels")
	}
	return modalStyle.Width(width).Render(strings.Join(lines, "\n"))
}

func truncateEnd(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func truncateMiddle(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width < 5 {
		return truncateEnd(value, width)
	}
	left := (width - 1) / 2
	right := width - left - 1
	return string(runes[:left]) + "…" + string(runes[len(runes)-right:])
}
