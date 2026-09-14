package vfs

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalRecursiveCopyAndMove(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	destinationRoot := filepath.Join(root, "destination")
	movedRoot := filepath.Join(root, "moved")
	for _, directory := range []string{filepath.Join(sourceRoot, "folder", "child"), destinationRoot, movedRoot} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	input := filepath.Join(sourceRoot, "folder", "child", "file.txt")
	if err := os.WriteFile(input, []byte("streamed contents"), 0o640); err != nil {
		t.Fatal(err)
	}
	backend := NewLocal()
	entry, err := backend.Stat(context.Background(), filepath.Join(sourceRoot, "folder"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Copy(context.Background(), backend, entry, backend, destinationRoot, nil); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(destinationRoot, "folder", "child", "file.txt")
	data, err := os.ReadFile(copied)
	if err != nil || string(data) != "streamed contents" {
		t.Fatalf("copied data = %q, err = %v", data, err)
	}
	copiedEntry, err := backend.Stat(context.Background(), filepath.Join(destinationRoot, "folder"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Move(context.Background(), backend, copiedEntry, backend, movedRoot, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(movedRoot, "folder", "child", "file.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destinationRoot, "folder")); !os.IsNotExist(err) {
		t.Fatalf("source still exists after move: %v", err)
	}
}

func TestConnectLocalFileOpensItsParent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	location, err := Connect(context.Background(), file, "")
	if err != nil {
		t.Fatal(err)
	}
	if location.Path != filepath.Dir(file) {
		t.Fatalf("location path = %q", location.Path)
	}
}

func TestNormalizeFTPESLocation(t *testing.T) {
	tests := map[string]string{
		"FTPES, nas.ehazhub.hu:5021":       "ftpes://nas.ehazhub.hu:5021",
		"FTPES, user@nas.ehazhub.hu:5021/": "ftpes://user@nas.ehazhub.hu:5021/",
		"ftpes://user@nas.example:5021/":   "ftpes://user@nas.example:5021/",
	}
	for input, want := range tests {
		if got := NormalizeLocation(input); got != want {
			t.Errorf("NormalizeLocation(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFTPUsesCertificateServerNameOverride(t *testing.T) {
	parsed, err := url.Parse("ftpes://alice@nas.ehazhub.hu:5021/?tls-server-name=ehaziroda.myqnapcloud.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := ftpTLSServerName(parsed); got != "ehaziroda.myqnapcloud.com" {
		t.Fatalf("TLS server name = %q", got)
	}
	parsed.RawQuery = ""
	if got := ftpTLSServerName(parsed); got != "nas.ehazhub.hu" {
		t.Fatalf("default TLS server name = %q", got)
	}
}
