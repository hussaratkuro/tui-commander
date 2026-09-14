package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeLocationRemovesPassword(t *testing.T) {
	got := SanitizeLocation("sftp://alice:secret@example.com/work")
	if got != "sftp://alice@example.com/work" {
		t.Fatalf("SanitizeLocation() = %q", got)
	}
}

func TestSanitizeSMBLocationRemovesPassword(t *testing.T) {
	got := SanitizeLocation("smb://WORKGROUP;alice:secret@fileserver/Shared/folder")
	if got != "smb://WORKGROUP;alice@fileserver/Shared/folder" {
		t.Fatalf("SanitizeLocation() = %q", got)
	}
}

func TestSaveAndLoadBookmarksWithoutPassword(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	cfg := Config{}
	cfg.AddBookmark(Bookmark{Name: " Server ", Location: "ftps://alice:secret@example.com/files"})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configHome, "tui-commander", "config.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o", info.Mode().Perm())
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Bookmarks) != 1 || loaded.Bookmarks[0].Name != "Server" || loaded.Bookmarks[0].Location != "ftps://alice@example.com/files" {
		t.Fatalf("loaded config = %#v", loaded)
	}
}

func TestSaveAndLoadBookmarkWithOptInPassword(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	cfg := Config{}
	cfg.AddBookmark(Bookmark{
		Name: "Secure NAS", Location: "ftpes://alice:password-in-url@nas.example:5021/files",
		Password: "saved-secret",
	})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Bookmarks) != 1 {
		t.Fatalf("loaded bookmarks = %#v", loaded.Bookmarks)
	}
	bookmark := loaded.Bookmarks[0]
	if bookmark.Location != "ftpes://alice@nas.example:5021/files" || bookmark.Password != "saved-secret" {
		t.Fatalf("loaded bookmark = %#v", bookmark)
	}
	path := filepath.Join(configHome, "tui-commander", "config.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("password config mode = %o", info.Mode().Perm())
	}
}

func TestRenameBookmarkPreservesConnectionDetails(t *testing.T) {
	cfg := Config{Bookmarks: []Bookmark{
		{Name: "NAS", Location: "ftpes://alice@nas.example/files", Password: "secret"},
		{Name: "Server", Location: "sftp://bob@server.example/home/bob"},
	}}
	if err := cfg.RenameBookmark("NAS", "Archive NAS"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Bookmarks) != 2 || cfg.Bookmarks[0].Name != "Archive NAS" {
		t.Fatalf("renamed bookmarks = %#v", cfg.Bookmarks)
	}
	if cfg.Bookmarks[0].Location != "ftpes://alice@nas.example/files" || cfg.Bookmarks[0].Password != "secret" {
		t.Fatalf("connection details changed during rename: %#v", cfg.Bookmarks[0])
	}
	if err := cfg.RenameBookmark("Archive NAS", "server"); err == nil {
		t.Fatal("duplicate connection display name was accepted")
	}
}
