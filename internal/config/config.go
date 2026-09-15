package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Bookmark struct {
	Name          string `json:"name"`
	Group         string `json:"group,omitempty"`
	Location      string `json:"location"`
	Password      string `json:"password,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
}

type SessionTab struct {
	Location    string `json:"location"`
	LocalReturn string `json:"local_return,omitempty"`
	ShowHidden  bool   `json:"show_hidden,omitempty"`
}

type SessionPane struct {
	Tabs      []SessionTab `json:"tabs"`
	ActiveTab int          `json:"active_tab,omitempty"`
}

type Session struct {
	Panes [2]SessionPane `json:"panes"`
	Focus int            `json:"focus,omitempty"`
}

type Config struct {
	Bookmarks []Bookmark `json:"bookmarks,omitempty"`
	Session   *Session   `json:"session,omitempty"`
}

func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tui-commander", "config.json"), nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.normalize()
	return cfg, nil
}

func (c *Config) Save() error {
	c.normalize()
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (c *Config) AddBookmark(bookmark Bookmark) {
	bookmark.Name = strings.TrimSpace(bookmark.Name)
	bookmark.Location = SanitizeLocation(bookmark.Location)
	for i := range c.Bookmarks {
		if strings.EqualFold(c.Bookmarks[i].Name, bookmark.Name) {
			if bookmark.Group == "" {
				bookmark.Group = c.Bookmarks[i].Group
			}
			if bookmark.CredentialRef == "" && bookmark.Password == "" {
				bookmark.CredentialRef = c.Bookmarks[i].CredentialRef
			}
			c.Bookmarks[i] = bookmark
			c.normalize()
			return
		}
	}
	c.Bookmarks = append(c.Bookmarks, bookmark)
	c.normalize()
}

// SetBookmarkCredential links a bookmark to an encrypted gopass entry. An
// encrypted reference and a plaintext saved password are mutually exclusive.
func (c *Config) SetBookmarkCredential(index int, ref string) {
	if index < 0 || index >= len(c.Bookmarks) {
		return
	}
	c.Bookmarks[index].CredentialRef = strings.TrimSpace(ref)
	if c.Bookmarks[index].CredentialRef != "" {
		c.Bookmarks[index].Password = ""
	}
}

func (c *Config) RemoveBookmark(index int) {
	if index < 0 || index >= len(c.Bookmarks) {
		return
	}
	c.Bookmarks = append(c.Bookmarks[:index], c.Bookmarks[index+1:]...)
}

// MoveBookmark moves one bookmark by delta places and returns its new index.
// The slice order is the persisted user-defined order.
func (c *Config) MoveBookmark(index, delta int) int {
	if index < 0 || index >= len(c.Bookmarks) || delta == 0 {
		return index
	}
	target := max(0, min(len(c.Bookmarks)-1, index+delta))
	if target == index {
		return index
	}
	if !strings.EqualFold(c.Bookmarks[index].Group, c.Bookmarks[target].Group) {
		return index
	}
	bookmark := c.Bookmarks[index]
	if target < index {
		copy(c.Bookmarks[target+1:index+1], c.Bookmarks[target:index])
	} else {
		copy(c.Bookmarks[index:target], c.Bookmarks[index+1:target+1])
	}
	c.Bookmarks[target] = bookmark
	return target
}

// SetBookmarkGroup assigns a bookmark to a group and returns its new index.
// An empty group means the bookmark is ungrouped.
func (c *Config) SetBookmarkGroup(index int, group string) int {
	if index < 0 || index >= len(c.Bookmarks) {
		return index
	}
	group = strings.TrimSpace(group)
	for otherIndex, bookmark := range c.Bookmarks {
		if otherIndex != index && group != "" && strings.EqualFold(bookmark.Group, group) {
			group = bookmark.Group
			break
		}
	}
	selectedName := c.Bookmarks[index].Name
	c.Bookmarks[index].Group = group
	c.groupBookmarks()
	for newIndex, bookmark := range c.Bookmarks {
		if strings.EqualFold(bookmark.Name, selectedName) {
			return newIndex
		}
	}
	return index
}

// SortBookmarks orders bookmarks by group, protocol, and display name.
func (c *Config) SortBookmarks() {
	sort.SliceStable(c.Bookmarks, func(i, j int) bool {
		leftGroup, rightGroup := c.Bookmarks[i].Group, c.Bookmarks[j].Group
		if !strings.EqualFold(leftGroup, rightGroup) {
			return groupLess(leftGroup, rightGroup)
		}
		leftProtocol := bookmarkProtocol(c.Bookmarks[i].Location)
		rightProtocol := bookmarkProtocol(c.Bookmarks[j].Location)
		if leftProtocol != rightProtocol {
			return leftProtocol < rightProtocol
		}
		return strings.ToLower(c.Bookmarks[i].Name) < strings.ToLower(c.Bookmarks[j].Name)
	})
}

func (c *Config) RenameBookmark(oldName, newName string) error {
	oldName, newName = strings.TrimSpace(oldName), strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("connection name cannot be empty")
	}
	index := -1
	for i, bookmark := range c.Bookmarks {
		if strings.EqualFold(bookmark.Name, oldName) {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("connection %q no longer exists", oldName)
	}
	for i, bookmark := range c.Bookmarks {
		if i != index && strings.EqualFold(bookmark.Name, newName) {
			return fmt.Errorf("connection name %q already exists", newName)
		}
	}
	c.Bookmarks[index].Name = newName
	c.normalize()
	return nil
}

func (c *Config) normalize() {
	clean := c.Bookmarks[:0]
	for _, bookmark := range c.Bookmarks {
		bookmark.Name = strings.TrimSpace(bookmark.Name)
		bookmark.Group = strings.TrimSpace(bookmark.Group)
		bookmark.CredentialRef = strings.TrimSpace(bookmark.CredentialRef)
		if bookmark.CredentialRef != "" {
			bookmark.Password = ""
		}
		bookmark.Location = SanitizeLocation(bookmark.Location)
		if IsLocalLocation(bookmark.Location) {
			bookmark.Password = ""
			bookmark.CredentialRef = ""
		}
		if bookmark.Name != "" && bookmark.Location != "" {
			clean = append(clean, bookmark)
		}
	}
	c.Bookmarks = clean
	c.groupBookmarks()
	c.normalizeSession()
}

func (c *Config) normalizeSession() {
	if c.Session == nil {
		return
	}
	c.Session.Focus = max(0, min(1, c.Session.Focus))
	valid := true
	for paneIndex := range c.Session.Panes {
		pane := &c.Session.Panes[paneIndex]
		tabs := pane.Tabs[:0]
		for _, tab := range pane.Tabs {
			tab.Location = SanitizeLocation(strings.TrimSpace(tab.Location))
			tab.LocalReturn = strings.TrimSpace(tab.LocalReturn)
			if tab.Location != "" {
				tabs = append(tabs, tab)
			}
		}
		pane.Tabs = tabs
		if len(pane.Tabs) == 0 {
			valid = false
			continue
		}
		pane.ActiveTab = max(0, min(len(pane.Tabs)-1, pane.ActiveTab))
	}
	if !valid {
		c.Session = nil
	}
}

func (c *Config) groupBookmarks() {
	sort.SliceStable(c.Bookmarks, func(i, j int) bool {
		if strings.EqualFold(c.Bookmarks[i].Group, c.Bookmarks[j].Group) {
			return false
		}
		return groupLess(c.Bookmarks[i].Group, c.Bookmarks[j].Group)
	})
}

func groupLess(left, right string) bool {
	if left == "" {
		return false
	}
	if right == "" {
		return true
	}
	return strings.ToLower(left) < strings.ToLower(right)
}

func bookmarkProtocol(location string) string {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme == "" {
		return "local"
	}
	return strings.ToLower(parsed.Scheme)
}

func IsLocalLocation(location string) bool {
	parsed, err := url.Parse(strings.TrimSpace(location))
	return err == nil && (parsed.Scheme == "" || strings.EqualFold(parsed.Scheme, "file"))
}

func SanitizeLocation(location string) string {
	// URLs accepted by the connection dialog may contain a one-time password.
	// Keep credentials out of the URL. An explicitly opted-in password is stored
	// separately in Bookmark.Password so it cannot accidentally appear in paths,
	// status messages, or connection labels.
	if at := strings.LastIndex(location, "@"); at >= 0 {
		prefix := location[:at]
		if scheme := strings.Index(prefix, "://"); scheme >= 0 {
			credentials := prefix[scheme+3:]
			if colon := strings.Index(credentials, ":"); colon >= 0 {
				return prefix[:scheme+3] + credentials[:colon] + location[at:]
			}
		}
	}
	return location
}
