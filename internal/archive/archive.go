package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Format uint8

const (
	Unknown Format = iota
	SevenZip
	ZIP
	RAR
	GZIP
	TAR
	TARGZIP
)

func Detect(path string) Format {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return TARGZIP
	case strings.HasSuffix(lower, ".7z"):
		return SevenZip
	case strings.HasSuffix(lower, ".zip"):
		return ZIP
	case strings.HasSuffix(lower, ".rar"):
		return RAR
	case strings.HasSuffix(lower, ".tar"):
		return TAR
	case strings.HasSuffix(lower, ".gz"):
		return GZIP
	default:
		return Unknown
	}
}

func Supported(path string) bool { return Detect(path) != Unknown }

func Pack(ctx context.Context, inputs []string, output string) error {
	if len(inputs) == 0 {
		return fmt.Errorf("nothing selected")
	}
	format := Detect(output)
	if format == Unknown {
		return fmt.Errorf("unsupported archive extension: %s", output)
	}
	if format == GZIP && len(inputs) != 1 {
		return fmt.Errorf(".gz stores one file; use .tar.gz for multiple entries")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	workingDirectory := filepath.Dir(inputs[0])
	names := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if filepath.Dir(input) != workingDirectory {
			return fmt.Errorf("all archive inputs must share a directory")
		}
		names = append(names, filepath.Base(input))
	}
	var command *exec.Cmd
	switch format {
	case SevenZip, ZIP:
		args := append([]string{"a", "-y", output, "--"}, names...)
		command = exec.CommandContext(ctx, "7z", args...)
	case RAR:
		args := append([]string{"a", "-r", output, "--"}, names...)
		command = exec.CommandContext(ctx, "rar", args...)
	case TAR:
		args := append([]string{"-cf", output, "--"}, names...)
		command = exec.CommandContext(ctx, "tar", args...)
	case TARGZIP:
		args := append([]string{"-czf", output, "--"}, names...)
		command = exec.CommandContext(ctx, "tar", args...)
	case GZIP:
		return gzipFile(ctx, inputs[0], output)
	}
	command.Dir = workingDirectory
	return run(command)
}

func Extract(ctx context.Context, input, destination string) error {
	format := Detect(input)
	if format == Unknown {
		return fmt.Errorf("unsupported archive extension: %s", input)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	var command *exec.Cmd
	switch format {
	case SevenZip, ZIP, RAR:
		command = exec.CommandContext(ctx, "7z", "x", "-y", "-o"+destination, "--", input)
	case TAR:
		command = exec.CommandContext(ctx, "tar", "-xf", input, "-C", destination)
	case TARGZIP:
		command = exec.CommandContext(ctx, "tar", "-xzf", input, "-C", destination)
	case GZIP:
		name := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		return gunzipFile(ctx, input, filepath.Join(destination, name))
	}
	return run(command)
}

func gzipFile(ctx context.Context, input, output string) error {
	source, err := os.Open(input)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.Create(output)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "gzip", "-c")
	command.Stdin = source
	command.Stdout = destination
	err = command.Run()
	closeErr := destination.Close()
	if err != nil {
		os.Remove(output)
		return err
	}
	return closeErr
}

func gunzipFile(ctx context.Context, input, output string) error {
	source, err := os.Open(input)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.Create(output)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "gzip", "-dc")
	command.Stdin = source
	command.Stdout = destination
	err = command.Run()
	closeErr := destination.Close()
	if err != nil {
		os.Remove(output)
		return err
	}
	return closeErr
}

func run(command *exec.Cmd) error {
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		message = err.Error()
	}
	return fmt.Errorf("archive command failed: %s", message)
}

func CopyFile(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
