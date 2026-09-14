package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectSupportedFormats(t *testing.T) {
	tests := map[string]Format{
		"files.7z": SevenZip, "files.zip": ZIP, "files.rar": RAR,
		"file.gz": GZIP, "files.tar": TAR, "files.tar.gz": TARGZIP, "files.tgz": TARGZIP,
	}
	for name, want := range tests {
		if got := Detect(name); got != want {
			t.Errorf("Detect(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPackAndExtractTarGzip(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	if err := os.MkdirAll(filepath.Join(source, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "folder", "file.txt"), []byte("archive contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "bundle.tar.gz")
	if err := Pack(context.Background(), []string{filepath.Join(source, "folder")}, archivePath); err != nil {
		t.Fatal(err)
	}
	if err := Extract(context.Background(), archivePath, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "folder", "file.txt"))
	if err != nil || string(data) != "archive contents" {
		t.Fatalf("extracted data = %q, err = %v", data, err)
	}
}

func TestGzipRejectsMultipleInputs(t *testing.T) {
	if err := Pack(context.Background(), []string{"one", "two"}, "files.gz"); err == nil {
		t.Fatal("multiple inputs were accepted for gzip")
	}
}
