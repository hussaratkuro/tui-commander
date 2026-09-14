package vfs

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type Local struct{}

func NewLocal() *Local { return &Local{} }

func (l *Local) ID() string               { return "local" }
func (l *Local) Label() string            { return "Local" }
func (l *Local) Clean(path string) string { return filepath.Clean(path) }
func (l *Local) Join(path string, parts ...string) string {
	return filepath.Join(append([]string{path}, parts...)...)
}
func (l *Local) Dir(path string) string  { return filepath.Dir(path) }
func (l *Local) Base(path string) string { return filepath.Base(path) }
func (l *Local) Close() error            { return nil }

func (l *Local) List(ctx context.Context, path string) ([]Entry, error) {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		info, err := dirEntry.Info()
		if err != nil {
			continue
		}
		entries = append(entries, Entry{
			Name: dirEntry.Name(), Path: filepath.Join(path, dirEntry.Name()),
			Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime(),
			Dir: dirEntry.IsDir(), Link: dirEntry.Type()&os.ModeSymlink != 0,
		})
	}
	SortEntries(entries)
	return entries, nil
}

func (l *Local) Stat(_ context.Context, path string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Name: info.Name(), Path: path, Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime(), Dir: info.IsDir()}, nil
}

func (l *Local) Download(ctx context.Context, path string, destination io.Writer, progress func(int64)) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(destination, ContextReader(ctx, file, progress))
	return err
}

func (l *Local) Upload(ctx context.Context, path string, source io.Reader, mode fs.FileMode, progress func(int64)) error {
	if mode.Perm() == 0 {
		mode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tui-commander-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode.Perm()); err != nil {
		tmp.Close()
		return err
	}
	_, copyErr := io.Copy(tmp, ContextReader(ctx, source, progress))
	closeErr := tmp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmpName, path)
}

func (l *Local) Mkdir(_ context.Context, path string, mode fs.FileMode) error {
	if mode.Perm() == 0 {
		mode = 0o755
	}
	return os.MkdirAll(path, mode.Perm())
}

func (l *Local) Remove(_ context.Context, path string, recursive bool) error {
	if recursive {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func (l *Local) Rename(_ context.Context, oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
