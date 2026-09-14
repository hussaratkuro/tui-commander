package trash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPutListRestoreAndPermanentDelete(t *testing.T) {
	root := t.TempDir()
	backend, err := NewAt(filepath.Join(root, "Trash"))
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(root, "documents")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(sourceDir, "report.txt")
	if err := os.WriteFile(first, []byte("restorable"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("source still exists after trashing: %v", err)
	}
	entries, err := backend.List(context.Background(), Root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "report.txt" || entries[0].OriginalPath != first {
		t.Fatalf("recycle bin entries = %#v", entries)
	}
	if err := backend.Restore(context.Background(), entries[0].Path); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(first)
	if err != nil || string(content) != "restorable" {
		t.Fatalf("restored content = %q, err %v", content, err)
	}

	second := filepath.Join(sourceDir, "delete-me")
	if err := os.MkdirAll(filepath.Join(second, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "nested", "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	entries, err = backend.List(context.Background(), Root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("recycle bin after directory put = %#v, err %v", entries, err)
	}
	if err := backend.Remove(context.Background(), entries[0].Path, true); err != nil {
		t.Fatal(err)
	}
	entries, err = backend.List(context.Background(), Root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("recycle bin after permanent delete = %#v, err %v", entries, err)
	}
}

func TestRestoreRefusesToOverwrite(t *testing.T) {
	root := t.TempDir()
	backend, err := NewAt(filepath.Join(root, "Trash"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "same.txt")
	if err := os.WriteFile(source, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := backend.List(context.Background(), Root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %#v, err %v", entries, err)
	}
	if err := backend.Restore(context.Background(), entries[0].Path); err == nil {
		t.Fatal("restore overwrote an existing destination")
	}
	content, err := os.ReadFile(source)
	if err != nil || string(content) != "new" {
		t.Fatalf("existing destination changed to %q, err %v", content, err)
	}
}
