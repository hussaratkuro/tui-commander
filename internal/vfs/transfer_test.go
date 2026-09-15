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

func TestCopyAsAndMoveAsUseRequestedName(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	copyDir := filepath.Join(root, "copy")
	moveDir := filepath.Join(root, "move")
	for _, directory := range []string{sourceDir, copyDir, moveDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sourcePath := filepath.Join(sourceDir, "original.txt")
	if err := os.WriteFile(sourcePath, []byte("renamed transfer"), 0o640); err != nil {
		t.Fatal(err)
	}
	backend := NewLocal()
	entry, err := backend.Stat(context.Background(), sourcePath)
	if err != nil {
		t.Fatal(err)
	}

	if err := CopyAs(context.Background(), backend, entry, backend, copyDir, "copied.txt", nil); err != nil {
		t.Fatal(err)
	}
	copiedPath := filepath.Join(copyDir, "copied.txt")
	data, err := os.ReadFile(copiedPath)
	if err != nil || string(data) != "renamed transfer" {
		t.Fatalf("copied data = %q, err = %v", data, err)
	}

	copiedEntry, err := backend.Stat(context.Background(), copiedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := MoveAs(context.Background(), backend, copiedEntry, backend, moveDir, "moved.txt", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(copiedPath); !os.IsNotExist(err) {
		t.Fatalf("move source still exists: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(moveDir, "moved.txt"))
	if err != nil || string(data) != "renamed transfer" {
		t.Fatalf("moved data = %q, err = %v", data, err)
	}
}

func TestCopyAsRenamesDirectoryRootAndRejectsPathNames(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	destinationDir := filepath.Join(root, "destination")
	if err := os.MkdirAll(filepath.Join(sourceDir, "original", "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "original", "child", "note.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := NewLocal()
	entry, err := backend.Stat(context.Background(), filepath.Join(sourceDir, "original"))
	if err != nil {
		t.Fatal(err)
	}
	if err := CopyAs(context.Background(), backend, entry, backend, destinationDir, "renamed", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destinationDir, "renamed", "child", "note.txt")); err != nil {
		t.Fatal(err)
	}
	if err := CopyAs(context.Background(), backend, entry, backend, destinationDir, filepath.Join("nested", "name"), nil); err == nil {
		t.Fatal("CopyAs accepted a path instead of a destination name")
	}
}
