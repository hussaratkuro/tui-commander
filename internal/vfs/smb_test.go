package vfs

import (
	"net/url"
	"testing"
)

func TestParseSMBLocation(t *testing.T) {
	parsed, err := url.Parse("smb://WORKGROUP;alice:url-password@files.example:1445/Team%20Share/reports")
	if err != nil {
		t.Fatal(err)
	}
	location, err := parseSMBLocation(parsed, "prompt-password")
	if err != nil {
		t.Fatal(err)
	}
	if location.host != "files.example" || location.port != "1445" || location.domain != "WORKGROUP" || location.user != "alice" || location.share != "Team Share" {
		t.Fatalf("parsed SMB location = %#v", location)
	}
	if location.password != "prompt-password" {
		t.Fatalf("password = %q, want prompt password", location.password)
	}
}

func TestParseSMBLocationRequiresShare(t *testing.T) {
	parsed, err := url.Parse("smb://alice@files.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseSMBLocation(parsed, ""); err == nil {
		t.Fatal("accepted an SMB URL without a share")
	}
}

func TestSMBPathsStayInsideMountedShare(t *testing.T) {
	backend := &SMB{root: "/Shared"}
	if got := backend.Clean("/Shared/folder/../file.txt"); got != "/Shared/file.txt" {
		t.Fatalf("Clean() = %q", got)
	}
	if got := backend.Dir("/Shared"); got != "/Shared" {
		t.Fatalf("Dir(root) = %q", got)
	}
	if got := backend.sharePath("/Shared/folder/file.txt"); got != "folder/file.txt" {
		t.Fatalf("sharePath() = %q", got)
	}
}
