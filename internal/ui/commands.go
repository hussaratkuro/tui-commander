package ui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type commandID uint8

const (
	commandHelp commandID = iota
	commandFuzzy
	commandSync
	commandOpen
	commandOpenWith
	commandCopy
	commandMove
	commandMkdir
	commandRename
	commandTrash
	commandConnections
	commandLocation
	commandBookmark
	commandRefresh
	commandHidden
	commandTerminal
	commandRecycleBin
	commandMerger
	commandQuit
)

type commandItem struct {
	id    commandID
	label string
	keys  string
}

type commandPaletteState struct {
	query  string
	cursor int
	items  []commandItem
}

func (m *Model) openCommandPalette() {
	m.commands = commandPaletteState{items: []commandItem{
		{commandHelp, "Open keyboard help", "F1"},
		{commandFuzzy, "Find files with fzf", "F3"},
		{commandSync, "Synchronize the two directories", "Ctrl+S"},
		{commandOpen, "Open highlighted entry", "Enter"},
		{commandOpenWith, "Open highlighted file with application", "F4"},
		{commandCopy, "Copy selection to other pane", "F5"},
		{commandMove, "Move selection to other pane", "F6"},
		{commandMkdir, "Create directory", "F7"},
		{commandRename, "Rename highlighted entry", "F2"},
		{commandTrash, "Move selection to Recycle Bin or delete", "F8"},
		{commandConnections, "Open connections and bookmarks", "Ctrl+N"},
		{commandLocation, "Open path or network URL", "Ctrl+L"},
		{commandBookmark, "Bookmark active directory", "Ctrl+B"},
		{commandRefresh, "Refresh active pane and Git status", "Ctrl+R"},
		{commandHidden, "Toggle hidden files", "Ctrl+H"},
		{commandTerminal, "Toggle integrated terminal", "F9"},
		{commandRecycleBin, "Open or close Recycle Bin", "Alt+R"},
		{commandMerger, "Compare highlighted pane entries in merger", "Alt+M"},
		{commandQuit, "Quit tui-commander", "F10"},
	}}
	m.modal = modalCommands
}

func (m *Model) filteredCommands() []commandItem {
	result := make([]commandItem, 0, len(m.commands.items))
	for _, item := range m.commands.items {
		if fuzzyCommandMatch(item.label+" "+item.keys, m.commands.query) {
			result = append(result, item)
		}
	}
	return result
}

func fuzzyCommandMatch(label, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	label = strings.ToLower(label)
	position := 0
	for _, character := range query {
		found := strings.IndexRune(label[position:], character)
		if found < 0 {
			return false
		}
		position += found + 1
	}
	return true
}

func (m *Model) updateCommandPalette(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.filteredCommands()
	switch key.String() {
	case "esc", "ctrl+p", "ctrl+shift+p":
		m.modal, m.commands = modalNone, commandPaletteState{}
	case "up", "ctrl+k":
		m.commands.cursor = max(0, m.commands.cursor-1)
	case "down", "ctrl+j", "tab":
		m.commands.cursor = min(max(0, len(items)-1), m.commands.cursor+1)
	case "backspace":
		query := []rune(m.commands.query)
		if len(query) > 0 {
			m.commands.query = string(query[:len(query)-1])
			m.commands.cursor = 0
		}
	case "enter":
		if len(items) == 0 {
			return m, nil
		}
		item := items[min(m.commands.cursor, len(items)-1)]
		m.modal, m.commands = modalNone, commandPaletteState{}
		return m.executeCommand(item.id)
	default:
		if key.Type == tea.KeyRunes && !key.Alt {
			for _, character := range key.Runes {
				if unicode.IsPrint(character) {
					m.commands.query += string(character)
				}
			}
			m.commands.cursor = 0
		}
	}
	return m, nil
}

func (m *Model) executeCommand(id commandID) (tea.Model, tea.Cmd) {
	key := func(keyType tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: keyType} }
	switch id {
	case commandHelp:
		return m.handleMainKey(key(tea.KeyF1))
	case commandFuzzy:
		return m.handleMainKey(key(tea.KeyF3))
	case commandSync:
		return m, m.openSyncCenter()
	case commandOpen:
		return m.handleMainKey(key(tea.KeyEnter))
	case commandOpenWith:
		return m.handleMainKey(key(tea.KeyF4))
	case commandCopy:
		return m.handleMainKey(key(tea.KeyF5))
	case commandMove:
		return m.handleMainKey(key(tea.KeyF6))
	case commandMkdir:
		return m.handleMainKey(key(tea.KeyF7))
	case commandRename:
		return m.handleMainKey(key(tea.KeyF2))
	case commandTrash:
		return m.handleMainKey(key(tea.KeyF8))
	case commandConnections:
		return m.handleMainKey(key(tea.KeyCtrlN))
	case commandRefresh:
		return m.handleMainKey(key(tea.KeyCtrlR))
	case commandHidden:
		return m.handleMainKey(key(tea.KeyCtrlH))
	case commandTerminal:
		return m, m.toggleTerminal()
	case commandLocation:
		m.startPrompt("Location or connection URL", currentLocationString(*m.currentPane()), promptLocation, false)
	case commandBookmark:
		return m.handleMainKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	case commandRecycleBin:
		return m, m.openTrash()
	case commandMerger:
		return m.handleMainKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true})
	case commandQuit:
		return m.handleMainKey(key(tea.KeyF10))
	}
	return m, nil
}

func (m *Model) renderCommandPalette(width, height int) string {
	items := m.filteredCommands()
	limit := min(len(items), max(3, height-9))
	start := max(0, min(m.commands.cursor-limit/2, len(items)-limit))
	query := m.commands.query + "█"
	rows := []string{"Command palette", mutedStyle.Render("Fuzzy-search actions and shortcuts"), "", "> " + query, ""}
	if len(items) == 0 {
		rows = append(rows, mutedStyle.Render("No matching commands"))
	}
	for index := start; index < start+limit; index++ {
		item := items[index]
		keyWidth := 18
		line := truncateEnd(item.label, max(8, width-keyWidth-10)) + strings.Repeat(" ", max(1, width-keyWidth-10-lipgloss.Width(truncateEnd(item.label, max(8, width-keyWidth-10))))) + item.keys
		if index == m.commands.cursor {
			line = cursorStyle.Width(width - 6).Render(line)
		}
		rows = append(rows, line)
	}
	rows = append(rows, "", "Enter runs · ↑/↓ selects · Esc closes")
	return modalStyle.Width(width).Render(strings.Join(rows, "\n"))
}
