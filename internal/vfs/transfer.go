package vfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
)

type TransferProgress struct {
	Name  string
	Bytes int64
	Files int
	Total int64
}

type ProgressFunc func(TransferProgress)

func Copy(ctx context.Context, source Backend, sourceEntry Entry, destination Backend, destinationDir string, progress ProgressFunc) error {
	state := TransferProgress{Name: sourceEntry.Name, Total: treeSize(ctx, source, sourceEntry)}
	return copyEntry(ctx, source, sourceEntry, destination, destinationDir, &state, progress)
}

func Move(ctx context.Context, source Backend, sourceEntry Entry, destination Backend, destinationDir string, progress ProgressFunc) error {
	destinationPath := destination.Join(destinationDir, sourceEntry.Name)
	if source.ID() == destination.ID() {
		if err := source.Rename(ctx, sourceEntry.Path, destinationPath); err == nil {
			return nil
		}
		// Local moves can cross filesystem boundaries, where rename is not
		// available. Fall through to copy-and-delete in that case.
	}
	if err := Copy(ctx, source, sourceEntry, destination, destinationDir, progress); err != nil {
		return err
	}
	return source.Remove(ctx, sourceEntry.Path, sourceEntry.Dir)
}

func copyEntry(ctx context.Context, source Backend, entry Entry, destination Backend, destinationDir string, state *TransferProgress, progress ProgressFunc) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	destinationPath := destination.Join(destinationDir, entry.Name)
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
			if err := copyEntry(ctx, source, child, destination, destinationPath, state, progress); err != nil {
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
