package vfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"
)

type Entry struct {
	Name         string
	Path         string
	OriginalPath string
	Size         int64
	Mode         fs.FileMode
	ModTime      time.Time
	Dir          bool
	Link         bool
}

type Backend interface {
	ID() string
	Label() string
	Clean(string) string
	Join(string, ...string) string
	Dir(string) string
	Base(string) string
	List(context.Context, string) ([]Entry, error)
	Stat(context.Context, string) (Entry, error)
	Download(context.Context, string, io.Writer, func(int64)) error
	Upload(context.Context, string, io.Reader, fs.FileMode, func(int64)) error
	Mkdir(context.Context, string, fs.FileMode) error
	Remove(context.Context, string, bool) error
	Rename(context.Context, string, string) error
	Close() error
}

type Location struct {
	Backend Backend
	Path    string
	Raw     string
}

func SortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

func HumanSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == units[len(units)-1] {
			if value < 10 {
				return fmt.Sprintf("%.1f %s", value, unit)
			}
			return fmt.Sprintf("%.0f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d B", size)
}

type progressReader struct {
	r        io.Reader
	progress func(int64)
}

func (r progressReader) Read(buffer []byte) (int, error) {
	n, err := r.r.Read(buffer)
	if n > 0 && r.progress != nil {
		r.progress(int64(n))
	}
	return n, err
}

func ContextReader(ctx context.Context, reader io.Reader, progress func(int64)) io.Reader {
	return progressReader{r: &contextReader{ctx: ctx, reader: reader}, progress: progress}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}
