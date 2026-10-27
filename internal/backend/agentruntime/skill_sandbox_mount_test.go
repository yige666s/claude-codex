package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxHostBindSourceOnlyMountsSelectedSubdirectory(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	skill := filepath.Join(root, "skills")
	work := filepath.Join(data, "workspaces", "alice", "job-1")
	script := filepath.Join(skill, "demo")
	for _, dir := range []string{work, script} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	hostData := filepath.Join(root, "host", "agentapi_data")
	hostSkills := filepath.Join(root, "host", "skills")
	mounts := []dockerMount{
		{Type: "volume", Destination: data, Source: hostData},
		{Type: "bind", Destination: skill, Source: hostSkills},
		{Type: "bind", Destination: filepath.Join(root, "secrets"), Source: filepath.Join(root, "host", "secrets")},
	}
	got, err := sandboxHostBindSource(work, mounts)
	if err != nil || got != filepath.Join(hostData, "workspaces", "alice", "job-1") {
		t.Fatalf("workspace mount = %q, %v", got, err)
	}
	got, err = sandboxHostBindSource(script, mounts)
	if err != nil || got != filepath.Join(hostSkills, "demo") {
		t.Fatalf("skill mount = %q, %v", got, err)
	}
	if _, err = sandboxHostBindSource(filepath.Join(root, "unknown"), mounts); err == nil {
		t.Fatal("unmapped path was accepted")
	}
}

func TestSandboxHostBindSourceRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "secrets"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := sandboxHostBindSource(filepath.Join(root, "link", "key"), []dockerMount{{Type: "bind", Destination: root, Source: filepath.Join(root, "host")}})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink accepted: %v", err)
	}
}

func TestSandboxPathWithinRejectsSiblingAndRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	if sandboxPathWithin(root+"2"+string(filepath.Separator)+"alice", root, false) {
		t.Fatal("sibling accepted")
	}
	if sandboxPathWithin(root, root, false) {
		t.Fatal("whole user root accepted")
	}
	if !sandboxPathWithin(filepath.Join(root, "alice"), root, false) {
		t.Fatal("user workspace rejected")
	}
}

func TestDockerSkillShellRuntimeRejectsHostNetworkBeforeDocker(t *testing.T) {
	r := NewDockerSkillShellRuntime(SkillShellSandboxConfig{Runner: "docker", Network: "host"}, "bash", t.TempDir(), t.TempDir(), nil, []string{"Bash(echo *)"})
	r.commandBin = "nonexistent-docker-binary"
	_, err := r.ExecuteCommand(context.Background(), "echo unsafe")
	if err == nil || !strings.Contains(err.Error(), "host network") {
		t.Fatalf("host network was not rejected: %v", err)
	}
}
