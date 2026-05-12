package bash

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"claude-codex/internal/harness/permissions"
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
		{name: "sed stdout substitution without file", command: `sed 's/a/b/'`, want: true},
		{name: "sed in place is not read-only", command: `sed -i 's/a/b/' file.txt`, want: false},
		{name: "sed write command is not read-only", command: `sed -n 'w out.txt' file.txt`, want: false},
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

func TestApplyPython3Fallback(t *testing.T) {
	originalLookPath := lookPath
	defer func() { lookPath = originalLookPath }()
	lookPath = func(file string) (string, error) {
		switch file {
		case "python":
			return "", errors.New("not found")
		case "python3":
			return "/usr/bin/python3", nil
		default:
			return "", errors.New("unexpected")
		}
	}

	if got := applyPython3Fallback("python script.py"); got != "python3 script.py" {
		t.Fatalf("expected python3 fallback, got %q", got)
	}
	if got := applyPython3Fallback("cd repo && python script.py"); got != "cd repo && python3 script.py" {
		t.Fatalf("expected compound python3 fallback, got %q", got)
	}
}

func TestExecuteRejectsBackgroundMode(t *testing.T) {
	tool := NewTool(t.TempDir())
	input, _ := json.Marshal(map[string]any{"command": "echo hi", "run_in_background": true})
	_, err := tool.Execute(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "background execution is not supported") {
		t.Fatalf("expected background rejection, got %v", err)
	}
}
