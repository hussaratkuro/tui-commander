package openwith

import (
	"bufio"
	"errors"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Application struct {
	Name        string
	DesktopID   string
	DesktopFile string
	Default     bool
	Terminal    bool
}

type desktopEntry struct {
	name, mimeTypes, entryType, tryExec string
	noDisplay, hidden, terminal         bool
}

func MIMEType(path string) (string, error) {
	output, err := exec.Command("xdg-mime", "query", "filetype", path).Output()
	if err != nil {
		return "", fmt.Errorf("detect MIME type: %w", err)
	}
	mimeType := strings.TrimSpace(string(output))
	if mimeType == "" {
		return "application/octet-stream", nil
	}
	if mimeType == "inode/x-empty" || mimeType == "application/x-zerosize" {
		if inferred := mime.TypeByExtension(filepath.Ext(path)); inferred != "" {
			if mediaType, _, parseErr := mime.ParseMediaType(inferred); parseErr == nil {
				mimeType = mediaType
			}
		}
	}
	return mimeType, nil
}

func Applications(path string) ([]Application, string, error) {
	mimeType, err := MIMEType(path)
	if err != nil {
		return nil, "", err
	}
	defaultID := Default(mimeType)
	seen := make(map[string]bool)
	var applications []Application
	var fallback []Application
	for _, directory := range applicationDirectories() {
		filepath.WalkDir(directory, func(filePath string, item os.DirEntry, walkErr error) error {
			if walkErr != nil || item.IsDir() || !strings.HasSuffix(item.Name(), ".desktop") {
				return nil
			}
			relative, err := filepath.Rel(directory, filePath)
			if err != nil {
				return nil
			}
			desktopID := strings.ReplaceAll(filepath.ToSlash(relative), "/", "-")
			if seen[desktopID] {
				return nil
			}
			seen[desktopID] = true
			entry, err := parseDesktopEntry(filePath)
			if err != nil || entry.hidden || entry.noDisplay || entry.entryType != "Application" || entry.name == "" {
				return nil
			}
			if entry.tryExec != "" {
				if _, err := exec.LookPath(entry.tryExec); err != nil {
					return nil
				}
			}
			compatible := mimeListContains(entry.mimeTypes, mimeType)
			application := Application{
				Name: entry.name, DesktopID: desktopID, DesktopFile: filePath,
				Default: desktopID == defaultID, Terminal: entry.terminal,
			}
			fallback = append(fallback, application)
			if compatible || desktopID == defaultID {
				applications = append(applications, application)
			}
			return nil
		})
	}
	if len(applications) == 0 {
		applications = fallback
	}
	sort.SliceStable(applications, func(i, j int) bool {
		if applications[i].Default != applications[j].Default {
			return applications[i].Default
		}
		return strings.ToLower(applications[i].Name) < strings.ToLower(applications[j].Name)
	})
	return applications, mimeType, nil
}

func Default(mimeType string) string {
	output, err := exec.Command("xdg-mime", "query", "default", mimeType).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func SetDefault(application Application, mimeType string) error {
	if application.DesktopID == "" || mimeType == "" {
		return errors.New("application and MIME type are required")
	}
	output, err := exec.Command("xdg-mime", "default", application.DesktopID, mimeType).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set default: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func LaunchCommand(application Application, path string) *exec.Cmd {
	return exec.Command("gio", "launch", application.DesktopFile, path)
}

func DefaultLaunchCommand(path string) *exec.Cmd {
	return exec.Command("gio", "open", path)
}

func parseDesktopEntry(path string) (desktopEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return desktopEntry{}, err
	}
	defer file.Close()
	var entry desktopEntry
	inDesktopEntry := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inDesktopEntry = line == "[Desktop Entry]"
			continue
		}
		if !inDesktopEntry || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "Name":
			entry.name = value
		case "Type":
			entry.entryType = value
		case "MimeType":
			entry.mimeTypes = value
		case "TryExec":
			entry.tryExec = value
		case "NoDisplay":
			entry.noDisplay = strings.EqualFold(value, "true")
		case "Hidden":
			entry.hidden = strings.EqualFold(value, "true")
		case "Terminal":
			entry.terminal = strings.EqualFold(value, "true")
		}
	}
	return entry, scanner.Err()
}

func mimeListContains(list, mimeType string) bool {
	for _, candidate := range strings.Split(list, ";") {
		if candidate == mimeType {
			return true
		}
	}
	return false
}

func applicationDirectories() []string {
	var directories []string
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		directories = append(directories, filepath.Join(dataHome, "applications"))
	} else if home, err := os.UserHomeDir(); err == nil {
		directories = append(directories, filepath.Join(home, ".local", "share", "applications"))
	}
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	for _, directory := range filepath.SplitList(dataDirs) {
		directories = append(directories, filepath.Join(directory, "applications"))
	}
	return directories
}
