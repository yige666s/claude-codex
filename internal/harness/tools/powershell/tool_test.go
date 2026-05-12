package powershelltool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	toolkit "claude-codex/internal/harness/tools"
)

func executePowerShellTool(t *testing.T, tool toolkit.Tool, payload string) output {
	t.Helper()
	result, err := tool.Execute(context.Background(), json.RawMessage(payload))
	if err != nil {
		t.Fatalf("Execute(%s) error = %v", payload, err)
	}
	var out output
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output %q: %v", result.Output, err)
	}
	return out
}

func installFakePwsh(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	name := "pwsh"
	if runtime.GOOS == "windows" {
		name = "pwsh.bat"
		script = "@echo off\r\n" + script
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake pwsh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPowerShellToolExecutesCommandWithNonInteractiveShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script uses POSIX shell syntax")
	}
	installFakePwsh(t, "#!/bin/sh\nprintf '%s\\n' \"$@\"\n")
	tool := NewTool(t.TempDir())

	out := executePowerShellTool(t, tool, `{"command":"Write-Output hello","timeout":5000}`)
	if out.Interrupted {
		t.Fatalf("did not expect interruption: %#v", out)
	}
	if !strings.Contains(out.Stdout, "-NonInteractive") || !strings.Contains(out.Stdout, "Write-Output hello") {
		t.Fatalf("expected shell args in stdout, got %#v", out.Stdout)
	}
}

func TestPowerShellToolReturnsStderrAndExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script uses POSIX shell syntax")
	}
	installFakePwsh(t, "#!/bin/sh\necho boom >&2\nexit 7\n")
	tool := NewTool(t.TempDir())

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"Write-Error boom"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var out output
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output %q: %v", result.Output, err)
	}
	if out.ExitCode != 7 || !strings.Contains(out.Stderr, "boom") {
		t.Fatalf("expected exit 7 and stderr, got %#v", out)
	}
}

func TestPowerShellToolRejectsInvalidInput(t *testing.T) {
	tool := NewTool(t.TempDir())
	for _, tc := range []struct {
		payload string
		want    string
	}{
		{`{"command":""}`, "command is required"},
		{`{"command":"Start-Sleep 3"}`, "run blocking commands in the background"},
		{`{"command":"Write-Output ok","run_in_background":true}`, "background execution is not supported"},
		{`{"command":"Write-Output ok","timeout":999999999}`, "timeout exceeds"},
	} {
		_, err := tool.Execute(context.Background(), json.RawMessage(tc.payload))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("Execute(%s) error = %v, want %q", tc.payload, err, tc.want)
		}
	}
}

func TestPowerShellToolTimeoutInterruptsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script uses POSIX shell syntax")
	}
	installFakePwsh(t, "#!/bin/sh\nsleep 2\n")
	tool := NewTool(t.TempDir())

	start := time.Now()
	out := executePowerShellTool(t, tool, `{"command":"Write-Output slow","timeout":50}`)
	if !out.Interrupted {
		t.Fatalf("expected interrupted output, got %#v", out)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout took too long")
	}
}
