package ui

import (
	"path/filepath"
	"testing"
)

func TestParseArgsUsesTwoPositionalLocations(t *testing.T) {
	options, err := ParseArgs([]string{"/tmp/left", "/tmp/right"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Left != "/tmp/left" || options.Right != "/tmp/right" {
		t.Fatalf("options = %#v", options)
	}
}

func TestParseArgsDefaultsBothPanesToWorkingDirectory(t *testing.T) {
	options, err := ParseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if options.Left != options.Right || !filepath.IsAbs(options.Left) {
		t.Fatalf("options = %#v", options)
	}
	if !options.restoreSession || !options.saveSession {
		t.Fatalf("default session options = restore %v, save %v", options.restoreSession, options.saveSession)
	}
}

func TestParseArgsExplicitLocationsOverrideSessionButRemainSaveable(t *testing.T) {
	options, err := ParseArgs([]string{"/tmp/left", "/tmp/right"})
	if err != nil {
		t.Fatal(err)
	}
	if options.restoreSession || !options.saveSession {
		t.Fatalf("explicit session options = restore %v, save %v", options.restoreSession, options.saveSession)
	}
}

func TestParseArgsRejectsTooManyLocations(t *testing.T) {
	if _, err := ParseArgs([]string{"one", "two", "three"}); err == nil {
		t.Fatal("accepted too many locations")
	}
}
