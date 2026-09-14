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
