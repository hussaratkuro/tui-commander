package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type gitSummary struct {
	branch                    string
	staged, modified, unknown int
	ahead, behind             int
}

func readGitSummary(directory string) gitSummary {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "git", "-C", directory, "status", "--porcelain=v1", "--branch", "--untracked-files=normal").Output()
	if err != nil {
		return gitSummary{}
	}
	return parseGitSummary(string(output))
}

func parseGitSummary(output string) gitSummary {
	var summary gitSummary
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		if strings.HasPrefix(line, "## ") {
			header := strings.TrimPrefix(line, "## ")
			summary.branch = gitBranchName(header)
			summary.ahead = gitCounter(header, "ahead ")
			summary.behind = gitCounter(header, "behind ")
			continue
		}
		if len(line) < 2 {
			continue
		}
		if line[:2] == "??" {
			summary.unknown++
			continue
		}
		if line[0] != ' ' && line[0] != '?' {
			summary.staged++
		}
		if line[1] != ' ' && line[1] != '?' {
			summary.modified++
		}
	}
	return summary
}

func gitBranchName(header string) string {
	for _, prefix := range []string{"No commits yet on ", "Initial commit on "} {
		if strings.HasPrefix(header, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(header, prefix))
		}
	}
	if strings.HasPrefix(header, "HEAD ") {
		return "detached"
	}
	name := header
	if index := strings.Index(name, "..."); index >= 0 {
		name = name[:index]
	}
	if index := strings.IndexByte(name, ' '); index >= 0 {
		name = name[:index]
	}
	return strings.TrimSpace(name)
}

func gitCounter(header, label string) int {
	index := strings.Index(header, label)
	if index < 0 {
		return 0
	}
	value := header[index+len(label):]
	if end := strings.IndexAny(value, ",]"); end >= 0 {
		value = value[:end]
	}
	count, _ := strconv.Atoi(strings.TrimSpace(value))
	return count
}

func (s gitSummary) String() string {
	if s.branch == "" {
		return ""
	}
	parts := []string{"git:" + s.branch}
	if s.ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", s.ahead))
	}
	if s.behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", s.behind))
	}
	if s.staged > 0 {
		parts = append(parts, fmt.Sprintf("+%d", s.staged))
	}
	if s.modified > 0 {
		parts = append(parts, fmt.Sprintf("~%d", s.modified))
	}
	if s.unknown > 0 {
		parts = append(parts, fmt.Sprintf("?%d", s.unknown))
	}
	if len(parts) == 1 {
		parts = append(parts, "✓")
	}
	return strings.Join(parts, " ")
}
