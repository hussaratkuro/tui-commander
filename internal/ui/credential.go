package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type externalCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func resolveCredentialCmd(request credentialRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		executable, err := findGopass()
		if err != nil {
			return credentialResolvedMsg{request: request, err: err}
		}
		command := exec.CommandContext(ctx, executable, "credential", "get", "--ref", request.ref, "--password-stdin")
		command.Stdin = strings.NewReader(request.master + "\n")
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Run(); err != nil {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = err.Error()
			}
			return credentialResolvedMsg{request: request, err: fmt.Errorf("resolve %q: %s", request.ref, message)}
		}
		var credential externalCredential
		if err := json.Unmarshal(stdout.Bytes(), &credential); err != nil {
			return credentialResolvedMsg{request: request, err: fmt.Errorf("decode gopass response: %w", err)}
		}
		return credentialResolvedMsg{
			request: request, username: credential.Username, password: credential.Password,
		}
	}
}

func findGopass() (string, error) {
	if executable, err := exec.LookPath("gopass"); err == nil {
		return executable, nil
	}
	if current, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(current), "gopass")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("gopass was not found in PATH or beside tui-commander")
}

func locationWithCredentialUsername(raw, username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.User != nil && parsed.User.Username() != "" {
		return raw
	}
	parsed.User = url.User(username)
	return parsed.String()
}
