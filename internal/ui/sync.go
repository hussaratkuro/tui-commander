package ui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tui-commander/internal/vfs"
)

type syncDirection uint8

const (
	syncLeftToRight syncDirection = iota
	syncRightToLeft
	syncTwoWay
)

type syncItemStatus uint8

const (
	syncPending syncItemStatus = iota
	syncDone
	syncFailed
	syncConflict
)

type syncItem struct {
	rel        string
	kind       string
	reason     string
	sourceSide int
	entry      vfs.Entry
	status     syncItemStatus
	err        string
}

type syncCenterState struct {
	direction syncDirection
	checksum  bool
	items     []syncItem
	cursor    int
	offset    int
	scanning  bool
	running   bool
	err       string
}

type syncScannedMsg struct {
	direction syncDirection
	checksum  bool
	items     []syncItem
	err       error
}

type syncItemResult struct {
	index int
	err   error
}

type syncFinishedMsg struct {
	results []syncItemResult
}

type syncTreeEntry struct {
	rel   string
	entry vfs.Entry
	hash  [sha256.Size]byte
}

func (m *Model) openSyncCenter() tea.Cmd {
	if m.panes[0].location.Backend.ID() == "trash" || m.panes[1].location.Backend.ID() == "trash" {
		m.setStatus("Synchronization is unavailable for the Recycle Bin", true)
		return nil
	}
	if sameLocation(m.panes[0], m.panes[1]) {
		m.setStatus("The two panes already show the same directory", true)
		return nil
	}
	direction := syncLeftToRight
	if m.focus == 1 {
		direction = syncRightToLeft
	}
	m.sync = syncCenterState{direction: direction}
	m.modal = modalSync
	return m.startSyncScan()
}

func sameLocation(left, right *pane) bool {
	return left.location.Backend.ID() == right.location.Backend.ID() &&
		left.location.Backend.Clean(left.location.Path) == right.location.Backend.Clean(right.location.Path)
}

func (m *Model) startSyncScan() tea.Cmd {
	left, right := m.panes[0], m.panes[1]
	direction, checksum := m.sync.direction, m.sync.checksum
	m.sync.items, m.sync.err, m.sync.scanning, m.sync.running = nil, "", true, false
	ctx, cancel := context.WithCancel(context.Background())
	tick := m.startBusy("Comparing both directory trees", cancel)
	command := func() tea.Msg {
		leftTree, err := readSyncTree(ctx, left.location.Backend, left.location.Path, checksum)
		if err != nil {
			return syncScannedMsg{direction: direction, checksum: checksum, err: fmt.Errorf("left pane: %w", err)}
		}
		rightTree, err := readSyncTree(ctx, right.location.Backend, right.location.Path, checksum)
		if err != nil {
			return syncScannedMsg{direction: direction, checksum: checksum, err: fmt.Errorf("right pane: %w", err)}
		}
		return syncScannedMsg{direction: direction, checksum: checksum, items: buildSyncPlan(leftTree, rightTree, direction, checksum)}
	}
	return tea.Batch(command, tick)
}

func readSyncTree(ctx context.Context, backend vfs.Backend, root string, checksum bool) (map[string]syncTreeEntry, error) {
	result := make(map[string]syncTreeEntry)
	var walk func(string, string) error
	walk = func(directory, parentRel string) error {
		entries, err := backend.List(ctx, directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			rel := path.Join(parentRel, entry.Name)
			item := syncTreeEntry{rel: rel, entry: entry}
			if checksum && !entry.Dir {
				hasher := sha256.New()
				if err := backend.Download(ctx, entry.Path, hasher, nil); err != nil {
					return fmt.Errorf("checksum %s: %w", rel, err)
				}
				copy(item.hash[:], hasher.Sum(nil))
			}
			result[rel] = item
			if entry.Dir && !entry.Link {
				if err := walk(entry.Path, rel); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return result, walk(root, "")
}

func buildSyncPlan(left, right map[string]syncTreeEntry, direction syncDirection, checksum bool) []syncItem {
	keys := make([]string, 0, len(left)+len(right))
	seen := make(map[string]bool)
	for key := range left {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range right {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		leftDepth, rightDepth := strings.Count(keys[i], "/"), strings.Count(keys[j], "/")
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return strings.ToLower(keys[i]) < strings.ToLower(keys[j])
	})

	items := make([]syncItem, 0)
	blocked := make([]string, 0)
	addCopy := func(item syncTreeEntry, side int, reason string) {
		kind := "copy"
		if item.entry.Dir {
			kind = "mkdir"
		}
		items = append(items, syncItem{rel: item.rel, kind: kind, reason: reason, sourceSide: side, entry: item.entry})
	}
	addConflict := func(rel, reason string) {
		items = append(items, syncItem{rel: rel, kind: "conflict", reason: reason, status: syncConflict})
	}
	for _, key := range keys {
		isBlocked := false
		for _, prefix := range blocked {
			if strings.HasPrefix(key, prefix+"/") {
				isBlocked = true
				break
			}
		}
		if isBlocked {
			continue
		}
		l, leftOK := left[key]
		r, rightOK := right[key]
		switch direction {
		case syncLeftToRight, syncRightToLeft:
			source, sourceOK, destination, destinationOK, side := l, leftOK, r, rightOK, 0
			if direction == syncRightToLeft {
				source, sourceOK, destination, destinationOK, side = r, rightOK, l, leftOK, 1
			}
			if !sourceOK {
				continue // additive/update sync never deletes destination-only entries
			}
			if !destinationOK {
				addCopy(source, side, "missing on destination")
				continue
			}
			if source.entry.Dir != destination.entry.Dir {
				addConflict(key, "file/directory type mismatch")
				blocked = append(blocked, key)
				continue
			}
			if !source.entry.Dir && !syncFilesEqual(source, destination, checksum) {
				addCopy(source, side, "content or metadata differs")
			}
		case syncTwoWay:
			switch {
			case leftOK && !rightOK:
				addCopy(l, 0, "only on left")
			case rightOK && !leftOK:
				addCopy(r, 1, "only on right")
			case l.entry.Dir != r.entry.Dir:
				addConflict(key, "file/directory type mismatch")
				blocked = append(blocked, key)
			case l.entry.Dir:
				continue
			case syncFilesEqual(l, r, checksum):
				continue
			case l.entry.ModTime.After(r.entry.ModTime):
				addCopy(l, 0, "left is newer")
			case r.entry.ModTime.After(l.entry.ModTime):
				addCopy(r, 1, "right is newer")
			default:
				addConflict(key, "different files have the same timestamp")
			}
		}
	}
	return items
}

func syncFilesEqual(left, right syncTreeEntry, checksum bool) bool {
	if checksum {
		return left.hash == right.hash
	}
	return left.entry.Size == right.entry.Size && left.entry.ModTime.Unix() == right.entry.ModTime.Unix()
}

func (m *Model) handleSyncScanned(message syncScannedMsg) (tea.Model, tea.Cmd) {
	if m.modal != modalSync || message.direction != m.sync.direction || message.checksum != m.sync.checksum {
		return m, nil
	}
	m.sync.scanning = false
	if message.err != nil {
		m.sync.err = message.err.Error()
		m.setStatus("Sync comparison failed: "+message.err.Error(), true)
		return m, nil
	}
	m.sync.items = message.items
	m.sync.cursor, m.sync.offset = 0, 0
	m.setStatus(fmt.Sprintf("Sync preview ready · %d planned item(s)", len(message.items)), false)
	return m, nil
}

func (m *Model) updateSyncCenter(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		if key.String() == "esc" && m.cancel != nil {
			m.cancel()
			m.setStatus("Cancelling sync operation…", false)
		}
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.modal, m.sync = modalNone, syncCenterState{}
	case "up", "k":
		m.sync.cursor = max(0, m.sync.cursor-1)
	case "down", "j":
		m.sync.cursor = min(max(0, len(m.sync.items)-1), m.sync.cursor+1)
	case "left":
		m.sync.direction = (m.sync.direction + 2) % 3
		return m, m.startSyncScan()
	case "right", "d":
		m.sync.direction = (m.sync.direction + 1) % 3
		return m, m.startSyncScan()
	case "c":
		m.sync.checksum = !m.sync.checksum
		return m, m.startSyncScan()
	case "s":
		return m, m.startSyncScan()
	case "enter":
		return m, m.executeSync(false)
	case "r":
		return m, m.executeSync(true)
	}
	return m, nil
}

func (m *Model) executeSync(failedOnly bool) tea.Cmd {
	indices := make([]int, 0)
	items := append([]syncItem(nil), m.sync.items...)
	for index, item := range items {
		if item.status == syncConflict || item.status == syncDone {
			continue
		}
		if failedOnly && item.status != syncFailed {
			continue
		}
		indices = append(indices, index)
	}
	if len(indices) == 0 {
		if failedOnly {
			m.setStatus("No failed sync items to retry", false)
		} else {
			m.setStatus("No pending sync items", false)
		}
		return nil
	}
	left, right := m.panes[0], m.panes[1]
	ctx, cancel := context.WithCancel(context.Background())
	tick := m.startBusy(fmt.Sprintf("Synchronizing %d item(s)", len(indices)), cancel)
	progressChannel := m.progressCh
	m.sync.running = true
	command := func() tea.Msg {
		return syncFinishedMsg{results: runSyncQueue(ctx, left, right, items, indices, progressChannel)}
	}
	return tea.Batch(command, tick)
}

func runSyncQueue(ctx context.Context, left, right *pane, items []syncItem, indices []int, progressChannel chan vfs.TransferProgress) []syncItemResult {
	results := make([]syncItemResult, 0, len(indices))
	for _, index := range indices {
		item := items[index]
		source, destination := left, right
		if item.sourceSide == 1 {
			source, destination = right, left
		}
		destinationPath := syncDestinationPath(destination.location.Backend, destination.location.Path, item.rel)
		var err error
		if item.kind == "mkdir" {
			if existing, statErr := destination.location.Backend.Stat(ctx, destinationPath); statErr == nil && existing.Dir {
				err = nil
			} else {
				err = destination.location.Backend.Mkdir(ctx, destinationPath, item.entry.Mode)
			}
		} else {
			parentRel := path.Dir(item.rel)
			if parentRel == "." {
				parentRel = ""
			}
			destinationDir := syncDestinationPath(destination.location.Backend, destination.location.Path, parentRel)
			err = vfs.Copy(ctx, source.location.Backend, item.entry, destination.location.Backend, destinationDir, func(progress vfs.TransferProgress) {
				if progressChannel == nil {
					return
				}
				select {
				case progressChannel <- progress:
				default:
				}
			})
			if err == nil && destination.location.Backend.ID() == "local" && !item.entry.ModTime.IsZero() {
				err = os.Chtimes(destinationPath, item.entry.ModTime, item.entry.ModTime)
			}
		}
		results = append(results, syncItemResult{index: index, err: err})
		if ctx.Err() != nil {
			break
		}
	}
	return results
}

func syncDestinationPath(backend vfs.Backend, root, relative string) string {
	if relative == "" || relative == "." {
		return root
	}
	return backend.Join(root, strings.Split(relative, "/")...)
}

func (m *Model) handleSyncFinished(message syncFinishedMsg) (tea.Model, tea.Cmd) {
	failed := 0
	for _, result := range message.results {
		if result.index < 0 || result.index >= len(m.sync.items) {
			continue
		}
		if result.err != nil {
			m.sync.items[result.index].status = syncFailed
			m.sync.items[result.index].err = result.err.Error()
			failed++
		} else {
			m.sync.items[result.index].status = syncDone
			m.sync.items[result.index].err = ""
		}
	}
	m.sync.running = false
	if failed > 0 {
		m.setStatus(fmt.Sprintf("Sync finished with %d failed item(s) · press R to retry", failed), true)
	} else {
		m.setStatus("Sync queue complete · press S to compare again", false)
	}
	for index := range m.panes {
		m.panes[index].loading = true
	}
	return m, tea.Batch(m.loadPaneCmd(0), m.loadPaneCmd(1))
}

func (m *Model) renderSyncCenter(width, height int) string {
	direction := []string{"LEFT → RIGHT", "RIGHT → LEFT", "TWO-WAY"}[m.sync.direction]
	checksum := "metadata (size + time)"
	if m.sync.checksum {
		checksum = "SHA-256 content"
	}
	rows := []string{
		"Sync and transfer center",
		mutedStyle.Render("Dry-run preview; destination-only files are never deleted"),
		"",
		"Direction: " + direction + "   Compare: " + checksum,
		"",
	}
	if m.sync.scanning {
		rows = append(rows, "Scanning both directory trees…")
	} else if m.sync.err != "" {
		rows = append(rows, errorStyle.Render(truncateEnd(m.sync.err, width-6)))
	} else if len(m.sync.items) == 0 {
		rows = append(rows, "The directories are already synchronized.")
	} else {
		visible := max(3, height-12)
		if m.sync.cursor < m.sync.offset {
			m.sync.offset = m.sync.cursor
		}
		if m.sync.cursor >= m.sync.offset+visible {
			m.sync.offset = m.sync.cursor - visible + 1
		}
		m.sync.offset = max(0, min(m.sync.offset, max(0, len(m.sync.items)-visible)))
		end := min(len(m.sync.items), m.sync.offset+visible)
		for index := m.sync.offset; index < end; index++ {
			item := m.sync.items[index]
			arrow := "L→R"
			if item.sourceSide == 1 {
				arrow = "R→L"
			}
			state := "·"
			switch item.status {
			case syncDone:
				state = "✓"
			case syncFailed:
				state = "!"
			case syncConflict:
				state, arrow = "!", "SKIP"
			}
			detail := item.reason
			if item.err != "" {
				detail = item.err
			}
			line := fmt.Sprintf("%s %-4s %-6s %-*s  %s", state, arrow, item.kind, max(8, width/2-10), truncateEnd(item.rel, max(8, width/2-10)), detail)
			line = truncateEnd(line, width-6)
			if index == m.sync.cursor {
				line = cursorStyle.Width(width - 6).Render(line)
			}
			rows = append(rows, line)
		}
	}
	rows = append(rows, "", "←/→ direction · C checksum · Enter run/resume · R retry failed · S rescan · Esc close/cancel")
	return modalStyle.Width(width).Render(strings.Join(rows, "\n"))
}
