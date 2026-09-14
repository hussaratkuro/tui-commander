package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"tui-commander/internal/vfs"
)

type remotePathBackend struct{ *vfs.Local }

func (remotePathBackend) ID() string { return "ftp://alice@example.com" }

func TestLocationStringForPathIsFullAndOmitsPassword(t *testing.T) {
	pane := pane{location: vfs.Location{
		Backend: remotePathBackend{Local: vfs.NewLocal()},
		Raw:     "ftp://alice:secret@example.com/base",
	}}
	got := locationStringForPath(pane, "/folder/file.txt")
	if got != "ftp://alice@example.com/folder/file.txt" {
		t.Fatalf("full remote path = %q", got)
	}
}

func TestSMBLocationStringPreservesShareAndDomain(t *testing.T) {
	pane := pane{location: vfs.Location{
		Backend: remotePathBackend{Local: vfs.NewLocal()},
		Raw:     "smb://alice:secret@fileserver/Shared/base?domain=WORKGROUP",
	}}
	got := locationStringForPath(pane, "/Shared/folder/file.txt")
	if got != "smb://alice@fileserver/Shared/folder/file.txt?domain=WORKGROUP" {
		t.Fatalf("full SMB path = %q", got)
	}
}

func TestCopyToClipboardPassesExactPath(t *testing.T) {
	directory := t.TempDir()
	capture := filepath.Join(directory, "clipboard")
	helper := filepath.Join(directory, "wl-copy")
	script := "#!/bin/sh\ncat > \"$COMMANDER_CLIPBOARD_CAPTURE\"\n"
	if err := os.WriteFile(helper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("COMMANDER_CLIPBOARD_CAPTURE", capture)
	want := "/tmp/a path;$(not-a-command).txt"
	if err := copyToClipboard(want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("clipboard = %q, want %q", data, want)
	}
}

func TestCtrlAltCCopiesHighlightedPath(t *testing.T) {
	directory := t.TempDir()
	capture := filepath.Join(directory, "clipboard")
	helper := filepath.Join(directory, "wl-copy")
	script := "#!/bin/sh\ncat > \"$COMMANDER_CLIPBOARD_CAPTURE\"\n"
	if err := os.WriteFile(helper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("COMMANDER_CLIPBOARD_CAPTURE", capture)

	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	want := filepath.Join(directory, "highlighted file.txt")
	model.panes[0].entries = []vfs.Entry{{Name: filepath.Base(want), Path: want}}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true})
	if command == nil {
		t.Fatal("Ctrl+Alt+C returned no clipboard command")
	}
	message, ok := command().(clipboardCopiedMsg)
	if !ok || message.err != nil || message.path != want {
		t.Fatalf("clipboard message = %#v", message)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("clipboard = %q, want %q", data, want)
	}
}
