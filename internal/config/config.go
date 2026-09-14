package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Bookmark struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	Password string `json:"password,omitempty"`
}

type Config struct {
	Bookmarks []Bookmark `json:"bookmarks,omitempty"`
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
			c.Bookmarks[i] = bookmark
			c.normalize()
			return
		}
	}
	c.Bookmarks = append(c.Bookmarks, bookmark)
	c.normalize()
}

func (c *Config) RemoveBookmark(index int) {
	if index < 0 || index >= len(c.Bookmarks) {
		return
	}
	c.Bookmarks = append(c.Bookmarks[:index], c.Bookmarks[index+1:]...)
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
		bookmark.Location = SanitizeLocation(bookmark.Location)
		if bookmark.Name != "" && bookmark.Location != "" {
			clean = append(clean, bookmark)
		}
	}
	c.Bookmarks = clean
	sort.SliceStable(c.Bookmarks, func(i, j int) bool {
		return strings.ToLower(c.Bookmarks[i].Name) < strings.ToLower(c.Bookmarks[j].Name)
	})
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
