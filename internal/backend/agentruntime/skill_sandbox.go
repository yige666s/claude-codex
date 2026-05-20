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
	"sync"
	"time"

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
	warmPoolLabelKey               = "agentapi.skill_warm_pool"
	warmPoolLabelValue             = "true"
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
	WarmPoolSize   int
}

type SkillSandboxWarmResult struct {
	Image    string
	Pulled   bool
	Duration time.Duration
	Error    error
}

type SandboxExecutionStats struct {
	Runner    string
	Image     string
	Network   string
	Duration  time.Duration
	Startup   time.Duration
	FromPool  bool
	OutputLen int
}

type sandboxStatsProvider interface {
	LastSandboxStats() SandboxExecutionStats
}

type DockerSkillWarmPool struct {
	commandBin string
	config     SkillShellSandboxConfig
	mu         sync.Mutex
	containers map[string][]string
	leased     map[string]bool
	closed     bool
}

var defaultDockerSkillWarmPool *DockerSkillWarmPool

func SetDefaultDockerSkillWarmPool(pool *DockerSkillWarmPool) {
	defaultDockerSkillWarmPool = pool
}

func StartDockerSkillWarmPool(ctx context.Context, config SkillShellSandboxConfig, images []string, size int) (*DockerSkillWarmPool, error) {
	config = config.normalized()
	if !config.DockerEnabled() || size <= 0 || !runningInContainer() || containerHostname() == "" {
		return nil, nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, err
	}
	pool := &DockerSkillWarmPool{
		commandBin: "docker",
		config:     config,
		containers: make(map[string][]string),
		leased:     make(map[string]bool),
	}
	if err := pool.removeStaleContainers(ctx); err != nil {
		return nil, err
	}
	for _, image := range uniqueNonEmptyStrings(append([]string{config.Image}, images...)) {
		for i := 0; i < size; i++ {
			name, err := pool.createContainer(ctx, image)
			if err != nil {
				pool.Close(context.Background())
				return nil, err
			}
			key := poolKeyForConfig(config, image)
			pool.containers[key] = append(pool.containers[key], name)
		}
	}
	return pool, nil
}

func (p *DockerSkillWarmPool) Close(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.closed = true
	var names []string
	for _, items := range p.containers {
		names = append(names, items...)
	}
	p.containers = nil
	p.leased = nil
	p.mu.Unlock()
	for _, name := range names {
		_ = exec.CommandContext(ctx, p.commandBin, "rm", "-f", name).Run()
	}
}

func (p *DockerSkillWarmPool) createContainer(ctx context.Context, image string) (string, error) {
	name := "agentapi-skill-warm-" + strings.ReplaceAll(newSortableID(), ".", "-")
	args := []string{
		"create",
		"--name", name,
		"--label", warmPoolLabelKey + "=" + warmPoolLabelValue,
		"--network", p.config.Network,
		"--memory", p.config.Memory,
		"--cpus", p.config.CPUs,
		"--pids-limit", strconv.Itoa(p.config.PidsLimit),
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=" + p.config.TmpfsSize,
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--volumes-from", containerHostname(),
		"-e", "PYTHONDONTWRITEBYTECODE=1",
		image,
		"sh", "-c", "while :; do sleep 3600; done",
	}
	if out, err := exec.CommandContext(ctx, p.commandBin, args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("create warm sandbox container for %s: %w: %s", image, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.CommandContext(ctx, p.commandBin, "start", name).CombinedOutput(); err != nil {
		_ = exec.CommandContext(context.Background(), p.commandBin, "rm", "-f", name).Run()
		return "", fmt.Errorf("start warm sandbox container for %s: %w: %s", image, err, strings.TrimSpace(string(out)))
	}
	return name, nil
}

func (p *DockerSkillWarmPool) removeStaleContainers(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, p.commandBin, "ps", "-aq", "--filter", "label="+warmPoolLabelKey+"="+warmPoolLabelValue).Output()
	if err != nil {
		return fmt.Errorf("list stale warm sandbox containers: %w", err)
	}
	for _, name := range strings.Fields(string(out)) {
		if name == "" {
			continue
		}
		if out, err := exec.CommandContext(ctx, p.commandBin, "rm", "-f", name).CombinedOutput(); err != nil {
			return fmt.Errorf("remove stale warm sandbox container %s: %w: %s", name, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (p *DockerSkillWarmPool) acquire(config SkillShellSandboxConfig) (string, bool) {
	if p == nil {
		return "", false
	}
	key := poolKeyForConfig(config, config.Image)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return "", false
	}
	for _, name := range p.containers[key] {
		if !p.leased[name] {
			p.leased[name] = true
			return name, true
		}
	}
	return "", false
}

func (p *DockerSkillWarmPool) release(name string) {
	if p == nil || name == "" {
		return
	}
	p.mu.Lock()
	delete(p.leased, name)
	p.mu.Unlock()
}

func poolKeyForConfig(config SkillShellSandboxConfig, image string) string {
	config = config.normalized()
	return strings.Join([]string{image, config.Network, config.Memory, config.CPUs, strconv.Itoa(config.PidsLimit), config.TmpfsSize}, "\x00")
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

func WarmDockerSkillSandboxImages(ctx context.Context, images []string) []SkillSandboxWarmResult {
	images = uniqueNonEmptyStrings(images)
	results := make([]SkillSandboxWarmResult, 0, len(images))
	if len(images) == 0 {
		return results
	}
	if _, err := exec.LookPath("docker"); err != nil {
		for _, image := range images {
			results = append(results, SkillSandboxWarmResult{Image: image, Error: fmt.Errorf("docker is not available: %w", err)})
		}
		return results
	}
	for _, image := range images {
		started := time.Now()
		result := SkillSandboxWarmResult{Image: image}
		if err := exec.CommandContext(ctx, "docker", "image", "inspect", image).Run(); err == nil {
			result.Duration = time.Since(started)
			results = append(results, result)
			continue
		}
		result.Pulled = true
		if err := exec.CommandContext(ctx, "docker", "pull", image).Run(); err != nil {
			result.Error = err
		}
		result.Duration = time.Since(started)
		results = append(results, result)
		if ctx.Err() != nil {
			break
		}
	}
	return results
}

type DockerSkillShellRuntime struct {
	config     SkillShellSandboxConfig
	shell      skills.FrontmatterShell
	workspace  string
	skillRoot  string
	env        map[string]string
	allowed    []string
	commandBin string
	lastStats  SandboxExecutionStats
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
	commandWithMarker := sandboxReadyMarkerCommand(rewritten)
	if outputText, stats, ok, err := r.executeWarm(ctx, workspace, skillRoot, commandWithMarker); ok {
		r.lastStats = stats
		outputText = strings.TrimSpace(rewriteContainerPaths(outputText, workspace, skillRoot))
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return "", fmt.Errorf("docker skill shell command timed out: %q", command)
			}
			return "", fmt.Errorf("docker warm skill shell command failed for %q: %s", command, outputText)
		}
		return outputText, nil
	}
	args := r.dockerArgs(workspace, skillRoot, commandWithMarker)

	cmd := exec.CommandContext(ctx, r.commandBin, args...)
	var output sandboxLimitBuffer
	output.limit = r.config.MaxOutputBytes
	cmd.Stdout = &output
	cmd.Stderr = &output
	started := time.Now()
	err = cmd.Run()
	duration := time.Since(started)
	outputText, startup := splitSandboxReadyMarker(output.String(), started)
	r.lastStats = SandboxExecutionStats{
		Runner:    "docker",
		Image:     r.config.Image,
		Network:   r.config.Network,
		Duration:  duration,
		Startup:   startup,
		OutputLen: len(outputText),
	}
	outputText = strings.TrimSpace(rewriteContainerPaths(outputText, workspace, skillRoot))
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

func (r *DockerSkillShellRuntime) LastSandboxStats() SandboxExecutionStats {
	if r == nil {
		return SandboxExecutionStats{}
	}
	return r.lastStats
}

func (r *DockerSkillShellRuntime) executeWarm(ctx context.Context, workspace, skillRoot, command string) (string, SandboxExecutionStats, bool, error) {
	if r == nil || !r.useContainerVolumes() || defaultDockerSkillWarmPool == nil {
		return "", SandboxExecutionStats{}, false, nil
	}
	name, ok := defaultDockerSkillWarmPool.acquire(r.config)
	if !ok {
		return "", SandboxExecutionStats{}, false, nil
	}
	defer defaultDockerSkillWarmPool.release(name)
	args := []string{"exec", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "-w", skillRoot, "-e", "AGENT_WORKSPACE_DIR=" + workspace, "-e", "CLAUDE_SKILL_DIR=" + skillRoot}
	for _, key := range sortedMapKeys(r.env) {
		if key == "AGENT_WORKSPACE_DIR" || key == "CLAUDE_SKILL_DIR" {
			continue
		}
		args = append(args, "-e", key+"="+r.env[key])
	}
	args = append(args, name)
	if r.shell == skills.ShellBash {
		args = append(args, "bash", "-lc", command)
	} else {
		args = append(args, "sh", "-lc", command)
	}
	cmd := exec.CommandContext(ctx, r.commandBin, args...)
	var output sandboxLimitBuffer
	output.limit = r.config.MaxOutputBytes
	cmd.Stdout = &output
	cmd.Stderr = &output
	started := time.Now()
	err := cmd.Run()
	duration := time.Since(started)
	outputText, startup := splitSandboxReadyMarker(output.String(), started)
	stats := SandboxExecutionStats{
		Runner:    "docker",
		Image:     r.config.Image,
		Network:   r.config.Network,
		Duration:  duration,
		Startup:   startup,
		FromPool:  true,
		OutputLen: len(outputText),
	}
	if output.exceeded {
		return outputText, stats, true, fmt.Errorf("docker skill shell output exceeds max size of %d bytes", output.limit)
	}
	return outputText, stats, true, err
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

const sandboxReadyMarker = "__AGENT_SANDBOX_READY_MS__="

func sandboxReadyMarkerCommand(command string) string {
	return fmt.Sprintf("printf '%s%%s\\n' \"$(date +%%s%%3N 2>/dev/null || date +%%s000)\"; %s", sandboxReadyMarker, command)
}

func splitSandboxReadyMarker(output string, started time.Time) (string, time.Duration) {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, sandboxReadyMarker) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, sandboxReadyMarker))
		ms, err := strconv.ParseInt(value, 10, 64)
		startup := time.Duration(0)
		if err == nil {
			startup = time.UnixMilli(ms).Sub(started)
			if startup < 0 {
				startup = 0
			}
		}
		out := append([]string{}, lines[:i]...)
		out = append(out, lines[i+1:]...)
		return strings.Join(out, "\n"), startup
	}
	return output, 0
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

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" || seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
	}
	return out
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
