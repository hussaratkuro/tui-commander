package ui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type clipboardCopiedMsg struct {
	path string
	err  error
}

func copyPathCmd(path string) tea.Cmd {
	return func() tea.Msg {
		return clipboardCopiedMsg{path: path, err: copyToClipboard(path)}
	}
}

func copyToClipboard(value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type candidate struct {
		name string
		args []string
	}
	commands := []candidate{
		{name: "wl-copy"},
		{name: "xclip", args: []string{"-selection", "clipboard"}},
		{name: "xsel", args: []string{"--clipboard", "--input"}},
	}
	var lastErr error
	for _, candidate := range commands {
		if _, err := exec.LookPath(candidate.name); err != nil {
			continue
		}
		command := exec.CommandContext(ctx, candidate.name, candidate.args...)
		command.Stdin = strings.NewReader(value)
		if output, err := command.CombinedOutput(); err == nil {
			return nil
		} else {
			lastErr = fmt.Errorf("%s: %w: %s", candidate.name, err, strings.TrimSpace(string(output)))
		}
	}
	// OSC 52 is a portable fallback when no desktop clipboard helper exists.
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	if _, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", encoded); err == nil {
		return nil
	} else if lastErr == nil {
		lastErr = err
	}
	return lastErr
}
