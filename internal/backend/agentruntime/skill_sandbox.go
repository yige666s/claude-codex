package agentruntime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"claude-codex/internal/harness/skills"
)

const (
	DefaultSkillShellRunner        = "local"
	DefaultSkillSandboxImage       = "python:3.12-slim"
	DefaultSkillSandboxNetwork     = "none"
	DefaultSkillSandboxMemory      = "512m"
	DefaultSkillSandboxCPUs        = "1"
	DefaultSkillSandboxPidsLimit   = 128
	DefaultSkillSandboxTmpfsSize   = "64m"
	containerWorkspaceDir          = "/workspace"
	containerSkillDir              = "/skill"
	defaultSkillSandboxOutputLimit = 1 << 20
)

type SkillShellSandboxConfig struct {
	Runner         string
	Image          string
	Network        string
	Memory         string
	CPUs           string
	PidsLimit      int
	TmpfsSize      string
	MaxOutputBytes int
}

func (c SkillShellSandboxConfig) normalized() SkillShellSandboxConfig {
	if strings.TrimSpace(c.Runner) == "" {
		c.Runner = DefaultSkillShellRunner
	}
	if strings.TrimSpace(c.Image) == "" {
		c.Image = DefaultSkillSandboxImage
	}
	if strings.TrimSpace(c.Network) == "" {
		c.Network = DefaultSkillSandboxNetwork
	}
	if strings.TrimSpace(c.Memory) == "" {
		c.Memory = DefaultSkillSandboxMemory
	}
	if strings.TrimSpace(c.CPUs) == "" {
		c.CPUs = DefaultSkillSandboxCPUs
	}
	if c.PidsLimit <= 0 {
		c.PidsLimit = DefaultSkillSandboxPidsLimit
	}
	if strings.TrimSpace(c.TmpfsSize) == "" {
		c.TmpfsSize = DefaultSkillSandboxTmpfsSize
	}
	if c.MaxOutputBytes <= 0 {
		c.MaxOutputBytes = defaultSkillSandboxOutputLimit
	}
	return c
}

func (c SkillShellSandboxConfig) dockerEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(c.Runner), "docker")
}

func (c SkillShellSandboxConfig) DockerEnabled() bool {
	return c.dockerEnabled()
}

type DockerSkillShellRuntime struct {
	config     SkillShellSandboxConfig
	shell      skills.FrontmatterShell
	workspace  string
	skillRoot  string
	env        map[string]string
	allowed    []string
	commandBin string
}

func NewDockerSkillShellRuntime(config SkillShellSandboxConfig, shell skills.FrontmatterShell, workspace, skillRoot string, env map[string]string, allowedTools []string) *DockerSkillShellRuntime {
	config = config.normalized()
	if strings.TrimSpace(skillRoot) == "" {
		skillRoot = workspace
	}
	return &DockerSkillShellRuntime{
		config:     config,
		shell:      shell,
		workspace:  filepath.Clean(workspace),
		skillRoot:  filepath.Clean(skillRoot),
		env:        cloneStringMap(env),
		allowed:    append([]string(nil), allowedTools...),
		commandBin: "docker",
	}
}

func (r *DockerSkillShellRuntime) ValidateCommand(command string) error {
	return skills.ValidatePromptShellCommand(command, r.shell, r.allowed)
}

func (r *DockerSkillShellRuntime) ExecuteCommand(ctx context.Context, command string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("docker skill shell runtime is not configured")
	}
	if r.shell == skills.ShellPowerShell {
		return "", fmt.Errorf("docker skill shell runtime does not support powershell skills")
	}
	if _, err := exec.LookPath(r.commandBin); err != nil {
		return "", fmt.Errorf("docker skill shell runtime requires docker on PATH: %w", err)
	}
	workspace, err := filepath.Abs(r.workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	skillRoot, err := filepath.Abs(r.skillRoot)
	if err != nil {
		return "", fmt.Errorf("resolve skill root: %w", err)
	}
	rewritten := rewriteHostPaths(command, workspace, skillRoot)
	if r.useContainerVolumes() {
		rewritten = command
	}
	args := r.dockerArgs(workspace, skillRoot, rewritten)

	cmd := exec.CommandContext(ctx, r.commandBin, args...)
	var output sandboxLimitBuffer
	output.limit = r.config.MaxOutputBytes
	cmd.Stdout = &output
	cmd.Stderr = &output
	err = cmd.Run()
	outputText := strings.TrimSpace(rewriteContainerPaths(output.String(), workspace, skillRoot))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("docker skill shell command timed out: %q", command)
		}
		return "", fmt.Errorf("docker skill shell command failed for %q: %s", command, outputText)
	}
	if output.exceeded {
		return "", fmt.Errorf("docker skill shell output exceeds max size of %d bytes", output.limit)
	}
	return outputText, nil
}

func (r *DockerSkillShellRuntime) dockerArgs(workspace, skillRoot, command string) []string {
	args := []string{
		"run",
		"--rm",
		"--network", r.config.Network,
		"--memory", r.config.Memory,
		"--cpus", r.config.CPUs,
		"--pids-limit", strconv.Itoa(r.config.PidsLimit),
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=" + r.config.TmpfsSize,
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-e", "PYTHONDONTWRITEBYTECODE=1",
	}
	if r.useContainerVolumes() {
		args = append(args,
			"--volumes-from", containerHostname(),
			"-w", skillRoot,
			"-e", "AGENT_WORKSPACE_DIR="+workspace,
			"-e", "CLAUDE_SKILL_DIR="+skillRoot,
		)
	} else {
		args = append(args,
			"-v", workspace+":"+containerWorkspaceDir+":rw",
			"-v", skillRoot+":"+containerSkillDir+":ro",
			"-w", containerSkillDir,
			"-e", "AGENT_WORKSPACE_DIR="+containerWorkspaceDir,
			"-e", "CLAUDE_SKILL_DIR="+containerSkillDir,
		)
	}
	for _, key := range sortedMapKeys(r.env) {
		if key == "AGENT_WORKSPACE_DIR" || key == "CLAUDE_SKILL_DIR" {
			continue
		}
		args = append(args, "-e", key+"="+r.env[key])
	}
	args = append(args, r.config.Image)
	if r.shell == skills.ShellBash {
		args = append(args, "bash", "-lc", command)
	} else {
		args = append(args, "sh", "-lc", command)
	}
	return args
}

func (r *DockerSkillShellRuntime) useContainerVolumes() bool {
	return runningInContainer() && containerHostname() != ""
}

func runningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return false
}

func containerHostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

func rewriteHostPaths(command, workspace, skillRoot string) string {
	out := command
	for _, mapping := range []struct {
		host      string
		container string
	}{
		{skillRoot, containerSkillDir},
		{workspace, containerWorkspaceDir},
	} {
		host := filepath.Clean(mapping.host)
		out = strings.ReplaceAll(out, host, mapping.container)
		out = strings.ReplaceAll(out, filepath.ToSlash(host), mapping.container)
	}
	return out
}

func rewriteContainerPaths(output, workspace, skillRoot string) string {
	out := output
	out = strings.ReplaceAll(out, containerSkillDir, filepath.Clean(skillRoot))
	out = strings.ReplaceAll(out, containerWorkspaceDir, filepath.Clean(workspace))
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func sortedMapKeys(in map[string]string) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

type sandboxLimitBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *sandboxLimitBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.Buffer.Len()
	if remaining <= 0 {
		b.exceeded = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.exceeded = true
		_, _ = b.Buffer.Write(p[:remaining])
		return len(p), nil
	}
	_, _ = b.Buffer.Write(p)
	return len(p), nil
}
