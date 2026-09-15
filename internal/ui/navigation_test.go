package ui

import (
	"os"
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

func TestTypingJumpsAndCtrlFFilters(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.width, model.height = 120, 32
	model.panes[0].loading = false
	model.panes[0].entries = []vfs.Entry{
		{Name: "alpha", Path: filepath.Join(directory, "alpha"), Dir: true},
		{Name: "README.md", Path: filepath.Join(directory, "README.md")},
		{Name: "source.go", Path: filepath.Join(directory, "source.go")},
	}

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("so")})
	pane := model.panes[0]
	if pane.filter != "" || pane.search != "so" || len(pane.visibleEntries()) != 3 {
		t.Fatalf("typing narrowed the listing: filter=%q search=%q", pane.filter, pane.search)
	}
	if entry, ok := pane.current(); !ok || entry.Name != "source.go" {
		t.Fatalf("search did not jump: %#v, %v", entry, ok)
	}
	if footer := model.renderFooter(120); !strings.Contains(footer, `Search: "so"`) {
		t.Fatalf("search missing from footer: %q", footer)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ur")})
	if entry, ok := pane.current(); !ok || entry.Name != "source.go" {
		t.Fatalf("search lost the match: %#v, %v", entry, ok)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if pane.search != "" {
		t.Fatalf("moving the highlight kept the search: %q", pane.search)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ead")})
	if entry, ok := pane.current(); !ok || entry.Name != "README.md" {
		t.Fatalf("substring search did not jump: %#v, %v", entry, ok)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if pane.search != "" {
		t.Fatalf("Esc did not clear the search: %q", pane.search)
	}

	model.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	if !pane.filterInput {
		t.Fatal("Ctrl+F did not enable filter input")
	}
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ReAd")})
	if pane.filter != "ReAd" || len(pane.visibleEntries()) != 1 {
		t.Fatalf("filter = %q, visible = %d", pane.filter, len(pane.visibleEntries()))
	}
	if entry, ok := pane.current(); !ok || entry.Name != "README.md" {
		t.Fatalf("filtered current entry = %#v, %v", entry, ok)
	}
	if footer := model.renderFooter(120); !strings.Contains(footer, `Filter: "ReAd"`) {
		t.Fatalf("filter missing from footer: %q", footer)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if pane.filter != "ReA" {
		t.Fatalf("backspace did not edit the filter: %q", pane.filter)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if pane.filter != "" || pane.filterInput || len(pane.visibleEntries()) != 3 {
		t.Fatalf("Esc did not clear the filter: %q input=%v", pane.filter, pane.filterInput)
	}
}

func TestBackspaceOpensParentAndRevealsChild(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"apple", "banana", "cherry"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	child := filepath.Join(root, "banana")
	model, err := New(Options{Left: child, Right: root})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.width, model.height = 120, 32
	model.panes[0].loading = false

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if command == nil {
		t.Fatal("backspace did not start loading the parent")
	}
	if model.panes[0].location.Path != root || model.panes[0].revealPath != child {
		t.Fatalf("path = %q reveal = %q", model.panes[0].location.Path, model.panes[0].revealPath)
	}
	model.Update(command())
	if entry, ok := model.panes[0].current(); !ok || entry.Path != child {
		t.Fatalf("highlight after going up = %#v, %v", entry, ok)
	}
}

func TestZoomKeyDecoding(t *testing.T) {
	cases := map[string]int{
		"?CSI[53 55 52 49 51 59 53 117]?": 1,  // CSI 57413;5u
		"?CSI[53 55 52 49 50 59 53 117]?": -1, // CSI 57412;5u
		"?CSI[52 51 59 53 117]?":          1,  // CSI 43;5u
		"?CSI[50 55 59 53 59 52 53 126]?": -1, // CSI 27;5;45~
		"?CSI[51 59 53 126]?":             0,  // CSI 3;5~ (Ctrl+Delete)
	}
	for text, want := range cases {
		if got := zoomKey(promptCSIMessage(text)); got != want {
			t.Fatalf("zoomKey(%q) = %d, want %d", text, got, want)
		}
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

	model.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ReAd")})
	if model.panes[0].filter != "ReAd" {
		t.Fatalf("filter = %q", model.panes[0].filter)
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
