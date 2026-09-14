package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type fuzzyFinishedMsg struct {
	index     int
	pane      *pane
	base      string
	selection string
	local     bool
	err       error
}

func (m *Model) startFuzzyFinder() tea.Cmd {
	executable, err := exec.LookPath("fzf")
	if err != nil {
		m.setStatus("Fuzzy finder requires fzf in PATH", true)
		return nil
	}
	index := m.focus
	pane := m.panes[index]
	arguments := []string{
		"--height=70%", "--layout=reverse", "--border=rounded",
		"--border-label= Find files ", "--prompt=Find › ", "--scheme=path",
		"--print0", "--no-multi-line",
	}
	local := pane.location.Backend.ID() == "local"
	command := exec.Command(executable)
	command.Env = withoutEnvironment(os.Environ(), "FZF_DEFAULT_COMMAND")
	if local {
		command.Dir = pane.location.Path
		walker := "file,dir"
		if pane.showHidden {
			walker += ",hidden"
		}
		arguments = append(arguments, "--walker="+walker, "--walker-root=.")
	} else {
		var input bytes.Buffer
		for _, entry := range pane.entries {
			if !pane.showHidden && strings.HasPrefix(entry.Name, ".") {
				continue
			}
			input.WriteString(entry.Name)
			input.WriteByte(0)
		}
		if input.Len() == 0 {
			m.setStatus("No visible entries for the fuzzy finder", true)
			return nil
		}
		arguments = append(arguments, "--read0")
		command.Stdin = bytes.NewReader(input.Bytes())
	}
	command.Args = append([]string{executable}, arguments...)
	var output bytes.Buffer
	command.Stdout = &output
	m.setStatus("Fuzzy finder · Enter selects · Esc cancels", false)
	return tea.ExecProcess(command, func(runErr error) tea.Msg {
		return fuzzyFinishedMsg{
			index: index, pane: pane, base: pane.location.Path,
			selection: trimFuzzySelection(output.String()), local: local, err: runErr,
		}
	})
}

func withoutEnvironment(environment []string, name string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return result
}

func trimFuzzySelection(value string) string {
	return strings.TrimRight(value, "\x00\r\n")
}

func fuzzyCanceled(err error) bool {
	var exitError *exec.ExitError
	return errors.As(err, &exitError) && (exitError.ExitCode() == 1 || exitError.ExitCode() == 130)
}

func (m *Model) handleFuzzyFinished(message fuzzyFinishedMsg) (tea.Model, tea.Cmd) {
	if message.pane != m.panes[message.index] {
		return m, nil
	}
	if message.err != nil {
		if fuzzyCanceled(message.err) {
			m.setStatus("Fuzzy finder cancelled", false)
		} else {
			m.setStatus("Fuzzy finder: "+message.err.Error(), true)
		}
		return m, nil
	}
	if message.selection == "" {
		m.setStatus("Fuzzy finder returned no selection", false)
		return m, nil
	}
	pane := message.pane
	pane.filter, pane.cursor, pane.offset = "", 0, 0
	if !message.local {
		target := pane.location.Backend.Join(message.base, message.selection)
		for index, entry := range pane.visibleEntries() {
			if entry.Path == target {
				pane.cursor = index
				pane.clamp(m.visibleRows())
				m.setStatus("Fuzzy match: "+entry.Name, false)
				return m, nil
			}
		}
		m.setStatus("Fuzzy match is no longer in the directory", true)
		return m, nil
	}
	target := message.selection
	if !filepath.IsAbs(target) {
		target = filepath.Join(message.base, target)
	}
	target = filepath.Clean(target)
	info, err := os.Lstat(target)
	if err != nil {
		m.setStatus("Fuzzy match: "+err.Error(), true)
		return m, nil
	}
	pane.location.Path = filepath.Dir(target)
	pane.revealPath = target
	pane.loading = true
	m.setStatus(fmt.Sprintf("Fuzzy match: %s", info.Name()), false)
	return m, m.loadPaneCmd(message.index)
}
