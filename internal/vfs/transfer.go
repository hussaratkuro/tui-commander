package vfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

type TransferProgress struct {
	Name  string
	Bytes int64
	Files int
	Total int64
}

type ProgressFunc func(TransferProgress)

func Copy(ctx context.Context, source Backend, sourceEntry Entry, destination Backend, destinationDir string, progress ProgressFunc) error {
	return CopyAs(ctx, source, sourceEntry, destination, destinationDir, sourceEntry.Name, progress)
}

// CopyAs copies sourceEntry into destinationDir using destinationName for the
// top-level entry. Children of a renamed directory keep their original names.
func CopyAs(ctx context.Context, source Backend, sourceEntry Entry, destination Backend, destinationDir, destinationName string, progress ProgressFunc) error {
	destinationPath, err := namedDestination(destination, destinationDir, destinationName)
	if err != nil {
		return err
	}
	if source.ID() == destination.ID() && destination.Clean(sourceEntry.Path) == destination.Clean(destinationPath) {
		return fmt.Errorf("source and destination are the same: %s", sourceEntry.Path)
	}
	state := TransferProgress{Name: sourceEntry.Name, Total: treeSize(ctx, source, sourceEntry)}
	return copyEntryTo(ctx, source, sourceEntry, destination, destinationPath, &state, progress)
}

func Move(ctx context.Context, source Backend, sourceEntry Entry, destination Backend, destinationDir string, progress ProgressFunc) error {
	return MoveAs(ctx, source, sourceEntry, destination, destinationDir, sourceEntry.Name, progress)
}

// CreateEmpty creates a new, empty file in destinationDir. It deliberately
// refuses to replace an existing entry, since Backend.Upload may truncate one.
func CreateEmpty(ctx context.Context, destination Backend, destinationDir, name string, mode fs.FileMode) error {
	destinationPath, err := namedDestination(destination, destinationDir, name)
	if err != nil {
		return err
	}
	entries, err := destination.List(ctx, destinationDir)
	if err != nil {
		return fmt.Errorf("list %s: %w", destinationDir, err)
	}
	for _, entry := range entries {
		if entry.Name == name {
			return fmt.Errorf("file already exists: %s: %w", destinationPath, fs.ErrExist)
		}
	}
	return destination.Upload(ctx, destinationPath, strings.NewReader(""), mode, nil)
}

// MoveAs moves sourceEntry into destinationDir using destinationName for the
// top-level entry. It works both as a same-backend rename and across backends.
func MoveAs(ctx context.Context, source Backend, sourceEntry Entry, destination Backend, destinationDir, destinationName string, progress ProgressFunc) error {
	destinationPath, err := namedDestination(destination, destinationDir, destinationName)
	if err != nil {
		return err
	}
	if source.ID() == destination.ID() && destination.Clean(sourceEntry.Path) == destination.Clean(destinationPath) {
		return fmt.Errorf("source and destination are the same: %s", sourceEntry.Path)
	}
	if source.ID() == destination.ID() {
		if err := source.Rename(ctx, sourceEntry.Path, destinationPath); err == nil {
			return nil
		}
		// Local moves can cross filesystem boundaries, where rename is not
		// available. Fall through to copy-and-delete in that case.
	}
	if err := CopyAs(ctx, source, sourceEntry, destination, destinationDir, destinationName, progress); err != nil {
		return err
	}
	return source.Remove(ctx, sourceEntry.Path, sourceEntry.Dir)
}

func namedDestination(destination Backend, destinationDir, destinationName string) (string, error) {
	if destinationName == "" || destinationName == "." || destinationName == ".." || destination.Base(destinationName) != destinationName {
		return "", fmt.Errorf("invalid destination name %q", destinationName)
	}
	return destination.Join(destinationDir, destinationName), nil
}

func copyEntryTo(ctx context.Context, source Backend, entry Entry, destination Backend, destinationPath string, state *TransferProgress, progress ProgressFunc) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	state.Name = entry.Name
	if entry.Dir {
		if err := destination.Mkdir(ctx, destinationPath, entry.Mode); err != nil {
			return fmt.Errorf("create %s: %w", destinationPath, err)
		}
		children, err := source.List(ctx, entry.Path)
		if err != nil {
			return fmt.Errorf("list %s: %w", entry.Path, err)
		}
		for _, child := range children {
			childDestination := destination.Join(destinationPath, child.Name)
			if err := copyEntryTo(ctx, source, child, destination, childDestination, state, progress); err != nil {
				return err
			}
		}
		return nil
	}

	reader, writer := io.Pipe()
	downloadErr := make(chan error, 1)
	go func() {
		counter := &countingWriter{Writer: writer}
		err := source.Download(ctx, entry.Path, counter, nil)
		if err == nil {
			err = ValidateDownloadedSize(entry.Size, counter.Bytes)
		}
		writer.CloseWithError(err)
		downloadErr <- err
	}()
	err := destination.Upload(ctx, destinationPath, reader, entry.Mode, func(bytes int64) {
		state.Bytes += bytes
		if progress != nil {
			progress(*state)
		}
	})
	reader.CloseWithError(err)
	sourceErr := <-downloadErr
	if err != nil {
		return fmt.Errorf("write %s: %w", destinationPath, err)
	}
	if sourceErr != nil {
		return fmt.Errorf("read %s: %w", entry.Path, sourceErr)
	}
	state.Files++
	if progress != nil {
		progress(*state)
	}
	return nil
}

type countingWriter struct {
	io.Writer
	Bytes int64
}

func (w *countingWriter) Write(data []byte) (int, error) {
	written, err := w.Writer.Write(data)
	w.Bytes += int64(written)
	return written, err
}

// ValidateDownloadedSize catches silent empty transfers without rejecting a
// growing or concurrently rotated remote file solely due to a stale listing.
func ValidateDownloadedSize(expected, received int64) error {
	if expected > 0 && received == 0 {
		return fmt.Errorf("download returned no data; directory listing reported %s", HumanSize(expected))
	}
	return nil
}

func treeSize(ctx context.Context, backend Backend, entry Entry) int64 {
	if !entry.Dir {
		return entry.Size
	}
	children, err := backend.List(ctx, entry.Path)
	if err != nil {
		return 0
	}
	var size int64
	for _, child := range children {
		size += treeSize(ctx, backend, child)
	}
	return size
}

func ModeForUpload(mode fs.FileMode) fs.FileMode {
	if mode.Perm() == 0 {
		return 0o644
	}
	return mode.Perm()
}
