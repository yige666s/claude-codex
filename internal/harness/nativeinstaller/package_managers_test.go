package nativeinstaller

import "testing"

func TestParseOSRelease(t *testing.T) {
	release := ParseOSRelease("ID=ubuntu\nID_LIKE=debian test\n")
	if release.ID != "ubuntu" || len(release.IDLike) != 2 || release.IDLike[0] != "debian" {
		t.Fatalf("unexpected os-release %#v", release)
	}
}

func TestDetectFromExecPath(t *testing.T) {
	if got := DetectFromExecPath("/opt/homebrew/Caskroom/claude/bin/claude", nil); got != PMHomebrew {
		t.Fatalf("expected homebrew, got %q", got)
	}
	if got := DetectFromExecPath(`/Users/me/.asdf/installs/claude/bin/claude`, nil); got != PMAsdf {
		t.Fatalf("expected asdf, got %q", got)
	}
	release := &OSRelease{ID: "ubuntu", IDLike: []string{"debian"}}
	if got := DetectFromExecPath("/usr/bin/claude", release); got != PMDeb {
		t.Fatalf("expected deb, got %q", got)
	}
}
