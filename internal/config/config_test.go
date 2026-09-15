package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSaveAndLoadSessionSanitizesLocationsAndActiveTabs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := Config{Session: &Session{
		Panes: [2]SessionPane{
			{Tabs: []SessionTab{
				{Location: "/srv/left"},
				{Location: "ftp://alice:secret@example.com/files", LocalReturn: "/home/alice", ShowHidden: true},
			}, ActiveTab: 99},
			{Tabs: []SessionTab{{Location: "/srv/right"}}, ActiveTab: -2},
		},
		Focus: 7,
	}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Session == nil {
		t.Fatal("saved session was not loaded")
	}
	if loaded.Session.Focus != 1 || loaded.Session.Panes[0].ActiveTab != 1 || loaded.Session.Panes[1].ActiveTab != 0 {
		t.Fatalf("normalized session = %#v", loaded.Session)
	}
	remote := loaded.Session.Panes[0].Tabs[1]
	if remote.Location != "ftp://alice@example.com/files" || remote.LocalReturn != "/home/alice" || !remote.ShowHidden {
		t.Fatalf("loaded remote session tab = %#v", remote)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tui-commander", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatalf("session leaked password: %s", data)
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

func TestLocalBookmarkDropsPasswordAndCredentialReference(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := Config{Bookmarks: []Bookmark{{
		Name: "Local", Location: "/home/alice", Password: "unused", CredentialRef: "unused-gopass-entry",
	}}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Bookmarks) != 1 || loaded.Bookmarks[0].Password != "" || loaded.Bookmarks[0].CredentialRef != "" {
		t.Fatalf("local bookmark retained credentials: %#v", loaded.Bookmarks)
	}
	if !IsLocalLocation("file:///tmp") || !IsLocalLocation("/tmp") || IsLocalLocation("sftp://host/tmp") {
		t.Fatal("local bookmark location detection is incorrect")
	}
}

func TestRenameBookmarkPreservesConnectionDetails(t *testing.T) {
	cfg := Config{Bookmarks: []Bookmark{
		{Name: "NAS", Group: "Work", Location: "ftpes://alice@nas.example/files", Password: "secret"},
		{Name: "Server", Location: "sftp://bob@server.example/home/bob"},
	}}
	if err := cfg.RenameBookmark("NAS", "Archive NAS"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Bookmarks) != 2 || cfg.Bookmarks[0].Name != "Archive NAS" {
		t.Fatalf("renamed bookmarks = %#v", cfg.Bookmarks)
	}
	if cfg.Bookmarks[0].Group != "Work" || cfg.Bookmarks[0].Location != "ftpes://alice@nas.example/files" || cfg.Bookmarks[0].Password != "secret" {
		t.Fatalf("connection details changed during rename: %#v", cfg.Bookmarks[0])
	}
	if err := cfg.RenameBookmark("Archive NAS", "server"); err == nil {
		t.Fatal("duplicate connection display name was accepted")
	}
}

func TestBookmarkGroupsArePersistedAndKeptTogether(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := Config{Bookmarks: []Bookmark{
		{Name: "Home", Location: "/home/user"},
		{Name: "NAS logs", Group: "Work", Location: "ftpes://nas.example/logs"},
		{Name: "Photos", Group: "Personal", Location: "/srv/photos"},
		{Name: "Git", Group: "work", Location: "sftp://git.example/repos"},
	}}
	cfg.normalize()
	if got := []string{cfg.Bookmarks[0].Name, cfg.Bookmarks[1].Name, cfg.Bookmarks[2].Name, cfg.Bookmarks[3].Name}; got[0] != "Photos" || got[1] != "NAS logs" || got[2] != "Git" || got[3] != "Home" {
		t.Fatalf("grouped bookmark order = %v", got)
	}
	if index := cfg.SetBookmarkGroup(3, "WORK"); index != 3 || cfg.Bookmarks[index].Group != "Work" {
		t.Fatalf("assigned bookmark group = index %d, bookmarks %#v", index, cfg.Bookmarks)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Bookmarks) != 4 || loaded.Bookmarks[3].Name != "Home" || loaded.Bookmarks[3].Group != "Work" {
		t.Fatalf("persisted bookmark groups = %#v", loaded.Bookmarks)
	}
	if got := loaded.MoveBookmark(0, 1); got != 0 {
		t.Fatalf("bookmark moved outside its group to index %d", got)
	}
}

func TestBookmarkCredentialReferenceReplacesPlaintextPassword(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := Config{Bookmarks: []Bookmark{{
		Name: "NAS", Group: "Work", Location: "ftpes://alice@nas.example", Password: "plaintext",
	}}}
	cfg.SetBookmarkCredential(0, "nas-credential-id")
	if cfg.Bookmarks[0].Password != "" || cfg.Bookmarks[0].CredentialRef != "nas-credential-id" {
		t.Fatalf("linked bookmark = %#v", cfg.Bookmarks[0])
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Bookmarks) != 1 || loaded.Bookmarks[0].CredentialRef != "nas-credential-id" || loaded.Bookmarks[0].Password != "" {
		t.Fatalf("persisted credential reference = %#v", loaded.Bookmarks)
	}
	loaded.SetBookmarkCredential(0, "")
	if loaded.Bookmarks[0].CredentialRef != "" {
		t.Fatalf("credential reference was not removed: %#v", loaded.Bookmarks[0])
	}
}

func TestSaveAndLoadPreservesCustomBookmarkOrder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := Config{Bookmarks: []Bookmark{
		{Name: "Zulu", Location: "/srv/zulu"},
		{Name: "Alpha", Location: "/srv/alpha"},
	}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{loaded.Bookmarks[0].Name, loaded.Bookmarks[1].Name}; got[0] != "Zulu" || got[1] != "Alpha" {
		t.Fatalf("loaded bookmark order = %v, want [Zulu Alpha]", got)
	}
}

func TestMoveAndSortBookmarks(t *testing.T) {
	cfg := Config{Bookmarks: []Bookmark{
		{Name: "Work", Location: "sftp://example.com/work", Password: "secret"},
		{Name: "Zulu", Location: "/srv/zulu"},
		{Name: "Alpha", Location: "/srv/alpha"},
		{Name: "Archive", Location: "ftp://example.com/archive"},
	}}
	if got := cfg.MoveBookmark(0, 2); got != 2 {
		t.Fatalf("moved index = %d, want 2", got)
	}
	if cfg.Bookmarks[2].Name != "Work" || cfg.Bookmarks[2].Password != "secret" {
		t.Fatalf("move lost bookmark details: %#v", cfg.Bookmarks)
	}

	cfg.SortBookmarks()
	want := []string{"Archive", "Alpha", "Zulu", "Work"}
	for index, name := range want {
		if cfg.Bookmarks[index].Name != name {
			t.Fatalf("sorted bookmarks = %#v, want %v", cfg.Bookmarks, want)
		}
	}
}
