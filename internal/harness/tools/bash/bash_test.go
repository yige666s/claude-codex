package bash

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
)

func TestSplitSubcommandsRespectsQuotes(t *testing.T) {
	command := `echo "a;b" && ls | grep foo; printf 'x|y'`
	want := []string{`echo "a;b"`, "ls", "grep foo", `printf 'x|y'`}
	if got := splitSubcommands(command); !reflect.DeepEqual(got, want) {
		t.Fatalf("splitSubcommands(%q) = %#v, want %#v", command, got, want)
	}
}

func TestIsCommandReadOnly(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "git status", command: "git status", want: true},
		{name: "find without exec", command: `find . -name '*.go'`, want: true},
		{name: "find with delete", command: "find . -delete", want: false},
		{name: "unquoted expansion", command: "cat $HOME/.bashrc", want: false},
		{name: "output redirection", command: "ls > out.txt", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCommandReadOnly(tt.command); got != tt.want {
				t.Fatalf("IsCommandReadOnly(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestBashCommandIsSafe(t *testing.T) {
	controlChar := string([]byte{'e', 'c', 'h', 'o', ' ', 0x07})
	tests := []struct {
		name           string
		command        string
		wantBehavior   permissions.Behavior
		wantMisparsing bool
	}{
		{name: "empty command", command: "   ", wantBehavior: permissions.BehaviorAllow},
		{name: "control characters", command: controlChar, wantBehavior: permissions.BehaviorAsk, wantMisparsing: true},
		{name: "simple git commit", command: `git commit -m "fix typo"`, wantBehavior: permissions.BehaviorAllow},
		{name: "git commit substitution", command: `git commit -m "$(whoami)"`, wantBehavior: permissions.BehaviorAsk},
		{name: "safe heredoc substitution", command: "echo $(cat <<'EOF'\nhello\nEOF)", wantBehavior: permissions.BehaviorAllow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BashCommandIsSafe(tt.command)
			if result.Behavior != tt.wantBehavior {
				t.Fatalf("BashCommandIsSafe(%q) behavior = %q, want %q", tt.command, result.Behavior, tt.wantBehavior)
			}
			if result.IsBashSecurityCheckForMisparsing != tt.wantMisparsing {
				t.Fatalf("BashCommandIsSafe(%q) misparsing = %v, want %v", tt.command, result.IsBashSecurityCheckForMisparsing, tt.wantMisparsing)
			}
		})
	}
}

func TestCheckPathConstraintsDetectsDangerousTargets(t *testing.T) {
	cwd := t.TempDir()

	result := CheckPathConstraints("echo hi > /etc/passwd", cwd)
	if result.Behavior != permissions.BehaviorAsk || result.BlockedPath != "/etc/passwd" {
		t.Fatalf("unexpected output redirection result: %+v", result)
	}

	result = CheckPathConstraints("rm -rf /", cwd)
	if result.Behavior != permissions.BehaviorAsk || result.BlockedPath != "/" {
		t.Fatalf("unexpected removal result: %+v", result)
	}

	result = CheckPathConstraints("cat /etc/hosts", cwd)
	if result.Behavior != permissions.BehaviorPassthrough {
		t.Fatalf("expected read-only path command to pass through, got %+v", result)
	}
}

func TestResolvePathExpandsHomeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := resolvePath("~/notes.txt", "/tmp/work"); got != filepath.Join(home, "notes.txt") {
		t.Fatalf("resolvePath did not expand tilde: %q", got)
	}
	paths := extractCommandPaths("cd", nil, "/tmp/work")
	if len(paths) != 1 || paths[0] != home {
		t.Fatalf("expected cd with no args to target home dir, got %#v", paths)
	}
}
