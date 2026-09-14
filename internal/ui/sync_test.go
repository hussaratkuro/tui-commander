package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tui-commander/internal/vfs"
)

func TestCommandPaletteFindsSyncCenter(t *testing.T) {
	model := &Model{}
	model.openCommandPalette()
	model.commands.query = "sync dir"
	items := model.filteredCommands()
	if len(items) != 1 || items[0].id != commandSync {
		t.Fatalf("sync command match = %#v", items)
	}
}

func TestBuildSyncPlanDirectionsAndConflicts(t *testing.T) {
	now := time.Now()
	file := func(name string, size int64, changed time.Time) syncTreeEntry {
		return syncTreeEntry{rel: name, entry: vfs.Entry{Name: filepath.Base(name), Path: name, Size: size, ModTime: changed}}
	}
	left := map[string]syncTreeEntry{
		"left.txt":    file("left.txt", 4, now),
		"newer.txt":   file("newer.txt", 8, now.Add(time.Minute)),
		"conflict":    file("conflict", 2, now),
		"folder":      {rel: "folder", entry: vfs.Entry{Name: "folder", Path: "folder", Dir: true}},
		"folder/a.md": file("folder/a.md", 1, now),
	}
	right := map[string]syncTreeEntry{
		"right.txt": file("right.txt", 4, now),
		"newer.txt": file("newer.txt", 7, now),
		"conflict":  {rel: "conflict", entry: vfs.Entry{Name: "conflict", Path: "conflict", Dir: true}},
	}

	oneWay := buildSyncPlan(left, right, syncLeftToRight, false)
	if !hasSyncItem(oneWay, "left.txt", 0, syncPending) || hasSyncRel(oneWay, "right.txt") {
		t.Fatalf("unexpected one-way plan: %#v", oneWay)
	}
	if !hasSyncItem(oneWay, "conflict", 0, syncConflict) {
		t.Fatalf("type mismatch was not preserved as a conflict: %#v", oneWay)
	}
	twoWay := buildSyncPlan(left, right, syncTwoWay, false)
	if !hasSyncItem(twoWay, "left.txt", 0, syncPending) || !hasSyncItem(twoWay, "right.txt", 1, syncPending) ||
		!hasSyncItem(twoWay, "newer.txt", 0, syncPending) {
		t.Fatalf("unexpected two-way plan: %#v", twoWay)
	}
}

func TestSyncPlanScansRecursivelyWithChecksums(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(left, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(left, "nested", "note.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := vfs.NewLocal()
	leftTree, err := readSyncTree(context.Background(), backend, left, true)
	if err != nil {
		t.Fatal(err)
	}
	rightTree, err := readSyncTree(context.Background(), backend, right, true)
	if err != nil {
		t.Fatal(err)
	}
	plan := buildSyncPlan(leftTree, rightTree, syncLeftToRight, true)
	if len(plan) != 2 || plan[0].kind != "mkdir" || plan[1].kind != "copy" {
		t.Fatalf("unexpected recursive plan: %#v", plan)
	}

	// Verify the destination path helper shared by local and network queues.
	if got := syncDestinationPath(backend, right, "nested/note.txt"); got != filepath.Join(right, "nested", "note.txt") {
		t.Fatalf("destination path = %q", got)
	}
	leftPane := &pane{location: vfs.Location{Backend: backend, Path: left}}
	rightPane := &pane{location: vfs.Location{Backend: backend, Path: right}}
	indices := []int{0, 1}
	results := runSyncQueue(context.Background(), leftPane, rightPane, plan, indices, nil)
	for _, result := range results {
		if result.err != nil {
			t.Fatalf("queue item %d: %v", result.index, result.err)
		}
	}
	data, err := os.ReadFile(filepath.Join(right, "nested", "note.txt"))
	if err != nil || string(data) != "payload" {
		t.Fatalf("copied file = %q, err=%v", data, err)
	}
	rescanned, err := readSyncTree(context.Background(), backend, right, true)
	if err != nil {
		t.Fatal(err)
	}
	if next := buildSyncPlan(leftTree, rescanned, syncLeftToRight, true); len(next) != 0 {
		t.Fatalf("completed queue still plans work: %#v", next)
	}
}

func hasSyncRel(items []syncItem, rel string) bool {
	for _, item := range items {
		if item.rel == rel {
			return true
		}
	}
	return false
}

func hasSyncItem(items []syncItem, rel string, side int, status syncItemStatus) bool {
	for _, item := range items {
		if item.rel == rel && item.sourceSide == side && item.status == status {
			return true
		}
	}
	return false
}
