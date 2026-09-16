package openwith

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDesktopEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "editor.desktop")
	data := `[Desktop Entry]
Type=Application
Name=Great Editor
MimeType=text/plain;text/markdown;
Terminal=false
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := parseDesktopEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if entry.name != "Great Editor" || entry.entryType != "Application" || !mimeListContains(entry.mimeTypes, "text/plain") {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestLaunchCommandDoesNotUseAShell(t *testing.T) {
	application := Application{DesktopFile: "/tmp/editor.desktop"}
	command := LaunchCommand(application, "/tmp/a file;touch nope")
	want := []string{"gio", "launch", "/tmp/editor.desktop", "/tmp/a file;touch nope"}
	if len(command.Args) != len(want) {
		t.Fatalf("args = %#v", command.Args)
	}
	for index := range want {
		if command.Args[index] != want[index] {
			t.Fatalf("args = %#v", command.Args)
		}
	}
}

func TestApplicationsInferEmptyTextFileTypeAndFallBackToAllApps(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	applicationsDir := filepath.Join(root, "data", "applications")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(applicationsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	xdgMime := `#!/bin/sh
if [ "$2" = "filetype" ]; then
    case "$3" in
        *.txt) echo inode/x-empty ;;
        *) echo application/x-unregistered ;;
    esac
elif [ "$2" = "default" ] && [ "$3" = "text/plain" ]; then
    echo editor.desktop
fi
`
	if err := os.WriteFile(filepath.Join(bin, "xdg-mime"), []byte(xdgMime), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDesktop := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(applicationsDir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeDesktop("editor.desktop", "[Desktop Entry]\nType=Application\nName=Text Editor\nMimeType=text/plain;\n")
	writeDesktop("viewer.desktop", "[Desktop Entry]\nType=Application\nName=Image Viewer\nMimeType=image/png;\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(root, "empty-data"))

	emptyText := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(emptyText, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	applications, mimeType, err := Applications(emptyText)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "text/plain" || len(applications) != 1 || applications[0].DesktopID != "editor.desktop" || !applications[0].Default {
		t.Fatalf("empty text applications = %#v, MIME = %q", applications, mimeType)
	}

	unknown := filepath.Join(root, "document.unknown-extension")
	if err := os.WriteFile(unknown, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	applications, mimeType, err = Applications(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "application/x-unregistered" || len(applications) != 2 {
		t.Fatalf("fallback applications = %#v, MIME = %q", applications, mimeType)
	}
}
