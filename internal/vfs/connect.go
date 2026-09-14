package vfs

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func Connect(ctx context.Context, raw, password string) (Location, error) {
	raw = NormalizeLocation(raw)
	if raw == "" {
		raw = "."
	}
	if !strings.Contains(raw, "://") {
		localPath, err := expandLocalPath(raw)
		if err != nil {
			return Location{}, err
		}
		backend := NewLocal()
		return Location{Backend: backend, Path: backend.Clean(localPath), Raw: localPath}, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return Location{}, err
	}
	if parsed.Scheme == "file" {
		localPath, err := expandLocalPath(parsed.Path)
		if err != nil {
			return Location{}, err
		}
		backend := NewLocal()
		return Location{Backend: backend, Path: backend.Clean(localPath), Raw: localPath}, nil
	}
	var backend Backend
	switch strings.ToLower(parsed.Scheme) {
	case "ftp", "ftps", "ftpes", "ftps+implicit":
		backend, err = NewFTP(ctx, parsed, password)
	case "sftp":
		backend, err = NewSFTP(ctx, parsed, password)
	case "smb":
		backend, err = NewSMB(ctx, parsed, password)
	default:
		return Location{}, fmt.Errorf("unsupported location scheme %q", parsed.Scheme)
	}
	if err != nil {
		return Location{}, err
	}
	remotePath := parsed.Path
	if remotePath == "" {
		remotePath = "/"
	}
	return Location{Backend: backend, Path: backend.Clean(remotePath), Raw: raw}, nil
}

// NormalizeLocation accepts the URL form used by tui-commander as well as the
// "FTPES, host:port" notation commonly copied from graphical file managers.
func NormalizeLocation(raw string) string {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, ",", 2)
	if len(parts) != 2 {
		return raw
	}
	scheme := strings.ToLower(strings.TrimSpace(parts[0]))
	switch scheme {
	case "ftp", "ftps", "ftpes", "sftp", "smb":
		target := strings.TrimSpace(parts[1])
		if target != "" && !strings.Contains(target, "://") {
			return scheme + "://" + target
		}
	}
	return raw
}

func expandLocalPath(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		absolute = filepath.Dir(absolute)
	}
	return filepath.Clean(absolute), nil
}
