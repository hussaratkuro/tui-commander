package vfs

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type silentDownloadBackend struct{ *Local }

func (silentDownloadBackend) Download(context.Context, string, io.Writer, func(int64)) error {
	return nil
}

func TestCopyDoesNotReplaceDestinationAfterSilentEmptyDownload(t *testing.T) {
	destinationDir := t.TempDir()
	destinationPath := filepath.Join(destinationDir, "error.log")
	if err := os.WriteFile(destinationPath, []byte("keep existing content"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := silentDownloadBackend{Local: NewLocal()}
	entry := Entry{Name: "error.log", Path: "/remote/error.log", Size: 4096, Mode: 0o600}
	if err := Copy(context.Background(), source, entry, NewLocal(), destinationDir, nil); err == nil {
		t.Fatal("silent empty download was accepted")
	}
	content, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "keep existing content" {
		t.Fatalf("existing destination was replaced with %q", content)
	}
}
