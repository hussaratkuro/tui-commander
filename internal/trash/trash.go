package trash

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tui-commander/internal/vfs"
)

const Root = "/"

// Backend exposes the user's FreeDesktop recycle bin as a read-only directory.
// Mutations are deliberately limited to Restore and Remove.
type Backend struct {
	filesDir string
	infoDir  string
}

func New() (*Backend, error) {
	dataDir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dataDir = filepath.Join(home, ".local", "share")
	}
	return NewAt(filepath.Join(dataDir, "Trash"))
}

func NewAt(root string) (*Backend, error) {
	backend := &Backend{
		filesDir: filepath.Join(root, "files"),
		infoDir:  filepath.Join(root, "info"),
	}
	for _, directory := range []string{backend.filesDir, backend.infoDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create recycle bin: %w", err)
		}
	}
	return backend, nil
}

func (b *Backend) ID() string                { return "trash" }
func (b *Backend) Label() string             { return "Recycle Bin" }
func (b *Backend) Clean(value string) string { return path.Clean("/" + strings.TrimPrefix(value, "/")) }
func (b *Backend) Join(base string, parts ...string) string {
	return path.Join(append([]string{base}, parts...)...)
}
func (b *Backend) Dir(value string) string  { return path.Dir(b.Clean(value)) }
func (b *Backend) Base(value string) string { return path.Base(b.Clean(value)) }
func (b *Backend) Close() error             { return nil }

func (b *Backend) List(ctx context.Context, directory string) ([]vfs.Entry, error) {
	if b.Clean(directory) != Root {
		return nil, errors.New("directories inside the recycle bin are restored as complete items")
	}
	infos, err := os.ReadDir(b.infoDir)
	if err != nil {
		return nil, err
	}
	entries := make([]vfs.Entry, 0, len(infos))
	for _, infoFile := range infos {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if infoFile.IsDir() || !strings.HasSuffix(infoFile.Name(), ".trashinfo") {
			continue
		}
		key := strings.TrimSuffix(infoFile.Name(), ".trashinfo")
		metadata, err := b.readInfo(key)
		if err != nil {
			continue
		}
		storedPath := filepath.Join(b.filesDir, key)
		fileInfo, err := os.Lstat(storedPath)
		if err != nil {
			continue
		}
		name := filepath.Base(metadata.originalPath)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = key
		}
		entries = append(entries, vfs.Entry{
			Name: name, Path: path.Join(Root, key), OriginalPath: metadata.originalPath,
			Size: fileInfo.Size(), Mode: fileInfo.Mode(), ModTime: metadata.deletedAt,
			Dir: fileInfo.IsDir(), Link: fileInfo.Mode()&os.ModeSymlink != 0,
		})
	}
	vfs.SortEntries(entries)
	return entries, nil
}

func (b *Backend) Stat(_ context.Context, virtualPath string) (vfs.Entry, error) {
	key, err := trashKey(virtualPath)
	if err != nil {
		return vfs.Entry{}, err
	}
	metadata, err := b.readInfo(key)
	if err != nil {
		return vfs.Entry{}, err
	}
	info, err := os.Lstat(filepath.Join(b.filesDir, key))
	if err != nil {
		return vfs.Entry{}, err
	}
	return vfs.Entry{
		Name: filepath.Base(metadata.originalPath), Path: path.Join(Root, key), OriginalPath: metadata.originalPath,
		Size: info.Size(), Mode: info.Mode(), ModTime: metadata.deletedAt,
		Dir: info.IsDir(), Link: info.Mode()&os.ModeSymlink != 0,
	}, nil
}

func (b *Backend) Download(ctx context.Context, virtualPath string, destination io.Writer, progress func(int64)) error {
	key, err := trashKey(virtualPath)
	if err != nil {
		return err
	}
	file, err := os.Open(filepath.Join(b.filesDir, key))
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(destination, vfs.ContextReader(ctx, file, progress))
	return err
}

func (b *Backend) Upload(context.Context, string, io.Reader, fs.FileMode, func(int64)) error {
	return errors.New("the recycle bin is read-only")
}

func (b *Backend) Mkdir(context.Context, string, fs.FileMode) error {
	return errors.New("directories cannot be created in the recycle bin")
}

func (b *Backend) Rename(context.Context, string, string) error {
	return errors.New("items cannot be renamed in the recycle bin")
}

func (b *Backend) Remove(ctx context.Context, virtualPath string, _ bool) error {
	key, err := trashKey(virtualPath)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	storedPath := filepath.Join(b.filesDir, key)
	if err := os.RemoveAll(storedPath); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(b.infoDir, key+".trashinfo")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Put moves a local file or directory into the FreeDesktop recycle bin.
func (b *Backend) Put(ctx context.Context, sourcePath string) error {
	absolute, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(absolute); err != nil {
		return err
	}
	if inside(absolute, b.filesDir) || inside(absolute, b.infoDir) {
		return errors.New("item is already inside the recycle bin")
	}
	key, err := b.availableKey(filepath.Base(absolute))
	if err != nil {
		return err
	}
	infoPath := filepath.Join(b.infoDir, key+".trashinfo")
	if err := writeInfo(infoPath, absolute, time.Now()); err != nil {
		return err
	}
	destination := filepath.Join(b.filesDir, key)
	if err := movePath(ctx, absolute, destination); err != nil {
		_ = os.Remove(infoPath)
		return fmt.Errorf("move to recycle bin: %w", err)
	}
	return nil
}

// Restore returns one top-level recycle-bin item to its recorded location.
func (b *Backend) Restore(ctx context.Context, virtualPath string) error {
	key, err := trashKey(virtualPath)
	if err != nil {
		return err
	}
	metadata, err := b.readInfo(key)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(metadata.originalPath); err == nil {
		return fmt.Errorf("restore destination already exists: %s", metadata.originalPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(metadata.originalPath), 0o755); err != nil {
		return err
	}
	storedPath := filepath.Join(b.filesDir, key)
	if err := movePath(ctx, storedPath, metadata.originalPath); err != nil {
		return fmt.Errorf("restore %s: %w", metadata.originalPath, err)
	}
	if err := os.Remove(filepath.Join(b.infoDir, key+".trashinfo")); err != nil {
		return fmt.Errorf("remove recycle-bin metadata: %w", err)
	}
	return nil
}

type info struct {
	originalPath string
	deletedAt    time.Time
}

func (b *Backend) readInfo(key string) (info, error) {
	file, err := os.Open(filepath.Join(b.infoDir, key+".trashinfo"))
	if err != nil {
		return info{}, err
	}
	defer file.Close()
	var metadata info
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Path="):
			metadata.originalPath, err = url.PathUnescape(strings.TrimPrefix(line, "Path="))
			if err != nil {
				return info{}, err
			}
		case strings.HasPrefix(line, "DeletionDate="):
			metadata.deletedAt, _ = time.ParseInLocation("2006-01-02T15:04:05", strings.TrimPrefix(line, "DeletionDate="), time.Local)
		}
	}
	if err := scanner.Err(); err != nil {
		return info{}, err
	}
	if !filepath.IsAbs(metadata.originalPath) {
		return info{}, errors.New("invalid recycle-bin metadata path")
	}
	return metadata, nil
}

func (b *Backend) availableKey(name string) (string, error) {
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "item"
	}
	for suffix := 0; ; suffix++ {
		key := name
		if suffix > 0 {
			key += "." + strconv.Itoa(suffix)
		}
		_, filesErr := os.Lstat(filepath.Join(b.filesDir, key))
		_, infoErr := os.Lstat(filepath.Join(b.infoDir, key+".trashinfo"))
		if errors.Is(filesErr, os.ErrNotExist) && errors.Is(infoErr, os.ErrNotExist) {
			return key, nil
		}
		if filesErr != nil && !errors.Is(filesErr, os.ErrNotExist) {
			return "", filesErr
		}
		if infoErr != nil && !errors.Is(infoErr, os.ErrNotExist) {
			return "", infoErr
		}
	}
}

func writeInfo(infoPath, originalPath string, deletedAt time.Time) error {
	file, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	content := "[Trash Info]\nPath=" + url.PathEscape(originalPath) + "\nDeletionDate=" + deletedAt.Format("2006-01-02T15:04:05") + "\n"
	_, writeErr := io.WriteString(file, content)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(infoPath)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(infoPath)
	}
	return closeErr
}

func trashKey(virtualPath string) (string, error) {
	clean := path.Clean("/" + strings.TrimPrefix(virtualPath, "/"))
	key := path.Base(clean)
	if clean == Root || key == "." || key == "/" || strings.ContainsRune(key, filepath.Separator) {
		return "", errors.New("select a recycle-bin item")
	}
	return key, nil
}

func inside(candidate, directory string) bool {
	relative, err := filepath.Rel(directory, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func movePath(ctx context.Context, source, destination string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := os.Rename(source, destination); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyPath(ctx, source, destination); err != nil {
		_ = os.RemoveAll(destination)
		return err
	}
	if err := os.RemoveAll(source); err != nil {
		_ = os.RemoveAll(destination)
		return err
	}
	return nil
}

func copyPath(ctx context.Context, source, destination string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(target, destination)
	}
	if info.IsDir() {
		if err := os.Mkdir(destination, info.Mode().Perm()); err != nil {
			return err
		}
		children, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := copyPath(ctx, filepath.Join(source, child.Name()), filepath.Join(destination, child.Name())); err != nil {
				return err
			}
		}
		return os.Chtimes(destination, info.ModTime(), info.ModTime())
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported file type: %s", info.Mode().Type())
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, vfs.ContextReader(ctx, input, nil))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chtimes(destination, info.ModTime(), info.ModTime())
}
