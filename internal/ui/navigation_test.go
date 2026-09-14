package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"tui-commander/internal/vfs"
)

func TestPaneFilteringAndHiddenFiles(t *testing.T) {
	directory := t.TempDir()
	pane := &pane{entries: []vfs.Entry{
		{Name: ".secret", Path: filepath.Join(directory, ".secret")},
		{Name: "README.md", Path: filepath.Join(directory, "README.md")},
		{Name: "src", Path: filepath.Join(directory, "src"), Dir: true},
	}}

	if got := entryNames(pane.visibleEntries()); strings.Join(got, ",") != "README.md,src" {
		t.Fatalf("default visible entries = %v", got)
	}
	pane.filter = "read"
	if got := entryNames(pane.visibleEntries()); len(got) != 1 || got[0] != "README.md" {
		t.Fatalf("filtered entries = %v", got)
	}
	pane.filter, pane.showHidden = "SECRET", true
	if got := entryNames(pane.visibleEntries()); len(got) != 1 || got[0] != ".secret" {
		t.Fatalf("hidden filtered entries = %v", got)
	}
}

func TestTypingFiltersAndCtrlHTogglesHiddenFiles(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.width, model.height = 120, 32
	model.panes[0].loading = false
	model.panes[0].entries = []vfs.Entry{
		{Name: ".secret", Path: filepath.Join(directory, ".secret")},
		{Name: "README.md", Path: filepath.Join(directory, "README.md")},
		{Name: "source.go", Path: filepath.Join(directory, "source.go")},
	}

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ReAd")})
	if model.panes[0].filter != "ReAd" {
		t.Fatalf("filter = %q", model.panes[0].filter)
	}
	if entry, ok := model.panes[0].current(); !ok || entry.Name != "README.md" {
		t.Fatalf("filtered current entry = %#v, %v", entry, ok)
	}
	if footer := model.renderFooter(120); !strings.Contains(footer, `Filter: "ReAd"`) {
		t.Fatalf("filter missing from footer: %q", footer)
	}

	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.panes[0].filter != "" {
		t.Fatalf("filter was not cleared: %q", model.panes[0].filter)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	if !model.panes[0].showHidden || len(model.panes[0].visibleEntries()) != 3 {
		t.Fatalf("Ctrl+H did not show hidden entries")
	}
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	if model.panes[0].showHidden || len(model.panes[0].visibleEntries()) != 2 {
		t.Fatalf("second Ctrl+H did not hide hidden entries")
	}
}

func TestGitSummaryParsing(t *testing.T) {
	summary := parseGitSummary("## main...origin/main [ahead 2, behind 1]\nM  staged.txt\n M modified.txt\n?? new.txt\n")
	if summary.branch != "main" || summary.ahead != 2 || summary.behind != 1 || summary.staged != 1 || summary.modified != 1 || summary.unknown != 1 {
		t.Fatalf("git summary = %#v", summary)
	}
	if got := summary.String(); got != "git:main ↑2 ↓1 +1 ~1 ?1" {
		t.Fatalf("formatted git summary = %q", got)
	}
}

func TestFuzzyHelpers(t *testing.T) {
	if got := trimFuzzySelection("./internal/ui/model.go\x00\n"); got != "./internal/ui/model.go" {
		t.Fatalf("trimFuzzySelection() = %q", got)
	}
	environment := withoutEnvironment([]string{"PATH=/bin", "FZF_DEFAULT_COMMAND=fd", "TERM=xterm"}, "FZF_DEFAULT_COMMAND")
	if got := strings.Join(environment, ";"); got != "PATH=/bin;TERM=xterm" {
		t.Fatalf("filtered environment = %q", got)
	}
}

func entryNames(entries []vfs.Entry) []string {
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name
	}
	return names
}
