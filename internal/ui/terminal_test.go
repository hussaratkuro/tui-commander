package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestIntegratedTerminalRunsInRequestedDirectory(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	directory := t.TempDir()
	session, err := startTerminalSession(directory, 200, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer session.close()
	session.emulator.SendText("printf '__TUI_COMMANDER_TERMINAL__:%s\\n' \"$PWD\"; exit\n")

	done := make(chan error, 1)
	go func() {
		for {
			message := readTerminalCmd(session)().(terminalOutputMsg)
			if len(message.data) > 0 {
				_, _ = session.emulator.Write(message.data)
			}
			if message.err != nil {
				break
			}
		}
		done <- session.command.Wait()
	}()
	select {
	case waitErr := <-done:
		if waitErr != nil {
			t.Fatal(waitErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("integrated terminal did not exit")
	}
	if screen := session.emulator.Render(); !strings.Contains(screen, "__TUI_COMMANDER_TERMINAL__:"+directory) {
		t.Fatalf("terminal output did not contain its working directory:\n%s", screen)
	}
}

func TestTerminalPanelPreservesOuterDimensions(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model.terminalVisible = true
	view := model.View()
	if !strings.Contains(view, "Terminal") {
		t.Fatalf("terminal panel missing:\n%s", view)
	}
	if got := lipgloss.Width(view); got != 120 {
		t.Fatalf("view width = %d, want 120", got)
	}
	if got := lipgloss.Height(view); got != 32 {
		t.Fatalf("view height = %d, want 32", got)
	}
}

func TestTerminalModifiedKeys(t *testing.T) {
	if got := modifiedTerminalSequence(tea.KeyCtrlLeft); got != "\x1b[1;5D" {
		t.Fatalf("Ctrl+Left sequence = %q", got)
	}
	if got := modifiedTerminalSequence(tea.KeyCtrlShiftEnd); got != "\x1b[1;6F" {
		t.Fatalf("Ctrl+Shift+End sequence = %q", got)
	}
}

func TestTerminalEnvironmentOverridesTerminalType(t *testing.T) {
	environment := terminalEnvironment([]string{"PATH=/bin", "TERM=dumb", "COLORTERM=old"})
	termCount, colorTermCount := 0, 0
	for _, entry := range environment {
		if strings.HasPrefix(entry, "TERM=") {
			termCount++
		}
		if strings.HasPrefix(entry, "COLORTERM=") {
			colorTermCount++
		}
	}
	joined := strings.Join(environment, "\n")
	if termCount != 1 || colorTermCount != 1 || !strings.Contains(joined, "TERM=xterm-256color") || !strings.Contains(joined, "COLORTERM=truecolor") {
		t.Fatalf("terminal environment = %#v", environment)
	}
}
