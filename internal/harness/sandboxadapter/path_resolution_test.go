package sandboxadapter

import "testing"

func TestResolvePathPatternForSandbox(t *testing.T) {
	root := "/tmp/settings"
	if got := ResolvePathPatternForSandbox("//.aws/**", root); got != "/.aws/**" {
		t.Fatalf("unexpected absolute root resolution %q", got)
	}
	if got := ResolvePathPatternForSandbox("/foo/bar", root); got != "/tmp/settings/foo/bar" {
		t.Fatalf("unexpected settings-relative resolution %q", got)
	}
}

func TestResolveSandboxFilesystemPath(t *testing.T) {
	root := "/tmp/settings"
	if got := ResolveSandboxFilesystemPath("//Users/me/.cargo", root); got != "/Users/me/.cargo" {
		t.Fatalf("unexpected legacy absolute resolution %q", got)
	}
	if got := ResolveSandboxFilesystemPath("relative/file", root); got != "/tmp/settings/relative/file" {
		t.Fatalf("unexpected relative resolution %q", got)
	}
}
