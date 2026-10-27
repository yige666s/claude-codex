package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
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
	defaultSkillShellPATH          = "/usr/local/bin:/usr/bin:/bin"
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
	WorkspaceRoot  string   // Container path of the per-user workspace root.
	SkillRoots     []string // Container paths of configured skill directories.
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
	// The old warm pool inherited *all* API mounts, including the daemon socket and
	// secrets. It cannot attach a per-task workspace after creation. Keep it off
	// until task-scoped prewarming moves to the dedicated Node Agent.
	if !config.DockerEnabled() || !runningInContainer() || containerHostname() == "" {
		return nil, nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, err
	}
	pool := &DockerSkillWarmPool{commandBin: "docker"}
	if err := pool.removeStaleContainers(ctx); err != nil {
		return nil, err
	}
	return nil, nil
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

type LocalSkillShellRuntime struct {
	config           SkillShellSandboxConfig
	shell            skills.FrontmatterShell
	workingDir       string
	skillRoot        string
	env              map[string]string
	networkAllowlist []string
	allowed          []string
	lastStats        SandboxExecutionStats
}

func NewLocalSkillShellRuntime(config SkillShellSandboxConfig, shell skills.FrontmatterShell, workingDir, skillRoot string, env map[string]string, networkAllowlist, allowedTools []string) *LocalSkillShellRuntime {
	config = config.normalized()
	if strings.TrimSpace(workingDir) == "" {
		workingDir = "."
	}
	if strings.TrimSpace(skillRoot) == "" {
		skillRoot = workingDir
	}
	return &LocalSkillShellRuntime{
		config:           config,
		shell:            shell,
		workingDir:       filepath.Clean(workingDir),
		skillRoot:        filepath.Clean(skillRoot),
		env:              cloneStringMap(env),
		networkAllowlist: append([]string(nil), networkAllowlist...),
		allowed:          append([]string(nil), allowedTools...),
	}
}

func (r *LocalSkillShellRuntime) ValidateCommand(command string) error {
	if r == nil {
		return fmt.Errorf("local skill shell runtime is not configured")
	}
	return skills.ValidatePromptShellCommand(command, r.shell, r.allowed)
}

func (r *LocalSkillShellRuntime) ExecuteCommand(ctx context.Context, command string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("local skill shell runtime is not configured")
	}
	if err := r.validateNetworkPolicy(); err != nil {
		return "", err
	}
	if err := r.ValidateCommand(command); err != nil {
		return "", err
	}
	exe := "bash"
	args := []string{"-lc", command}
	if r.shell == skills.ShellPowerShell {
		exe = "pwsh"
		args = []string{"-NoProfile", "-Command", command}
	}
	if strings.EqualFold(strings.TrimSpace(r.config.Network), DefaultSkillSandboxNetwork) {
		var err error
		exe, args, err = localNoNetworkCommand(exe, args)
		if err != nil {
			return "", err
		}
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = r.workingDir
	cmd.Env = localSkillShellEnvironment(r.workingDir, r.skillRoot, r.env)
	var output sandboxLimitBuffer
	output.limit = r.config.MaxOutputBytes
	started := time.Now()
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	duration := time.Since(started)
	outputText := strings.TrimSpace(output.String())
	r.lastStats = SandboxExecutionStats{
		Runner:    "local",
		Network:   r.config.Network,
		Duration:  duration,
		Startup:   0,
		OutputLen: len(outputText),
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("local skill shell command timed out: %q", command)
		}
		if outputText == "" {
			return "", fmt.Errorf("local skill shell command failed for %q: %v", command, err)
		}
		return "", fmt.Errorf("local skill shell command failed for %q: %s", command, outputText)
	}
	if output.exceeded {
		return "", fmt.Errorf("local skill shell output exceeds max size of %d bytes", output.limit)
	}
	return outputText, nil
}

func (r *LocalSkillShellRuntime) validateNetworkPolicy() error {
	if r == nil {
		return fmt.Errorf("local skill shell runtime is not configured")
	}
	if len(r.networkAllowlist) > 0 {
		return fmt.Errorf("local skill shell cannot enforce network allowlist %q; use a controlled container or proxy path", strings.Join(r.networkAllowlist, ","))
	}
	network := strings.ToLower(strings.TrimSpace(r.config.Network))
	if network != DefaultSkillSandboxNetwork && network != "host" {
		return fmt.Errorf("local skill shell supports only network=%q or explicit development network=%q; configured network %q needs a controlled container or proxy path", DefaultSkillSandboxNetwork, "host", network)
	}
	return nil
}

func localNoNetworkCommand(executable string, args []string) (string, []string, error) {
	switch goruntime.GOOS {
	case "darwin":
		sandbox, err := exec.LookPath("sandbox-exec")
		if err != nil {
			return "", nil, fmt.Errorf("local network isolation requires sandbox-exec: %w", err)
		}
		profile := "(version 1) (allow default) (deny network*)"
		return sandbox, append([]string{"-p", profile, executable}, args...), nil
	case "linux":
		unshare, err := exec.LookPath("unshare")
		if err != nil {
			return "", nil, fmt.Errorf("local network isolation requires unshare; use the docker runner: %w", err)
		}
		return unshare, append([]string{"--net", "--", executable}, args...), nil
	default:
		return "", nil, fmt.Errorf("local network isolation is unsupported on %s; use the docker runner", goruntime.GOOS)
	}
}

func (r *LocalSkillShellRuntime) LastSandboxStats() SandboxExecutionStats {
	if r == nil {
		return SandboxExecutionStats{}
	}
	return r.lastStats
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
	if strings.EqualFold(strings.TrimSpace(r.config.Network), "host") {
		return "", fmt.Errorf("host network is not allowed for skill sandbox")
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
	bindWorkspace, bindSkill := workspace, skillRoot
	if runningInContainer() {
		bindWorkspace, bindSkill, err = r.resolveContainerBindSources(ctx, workspace, skillRoot)
		if err != nil {
			return "", err
		}
	}
	commandWithMarker := sandboxReadyMarkerCommand(rewriteHostPaths(command, workspace, skillRoot))
	args := r.dockerArgs(bindWorkspace, bindSkill, commandWithMarker)

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

// dockerMount describes only the mount information returned by docker inspect.
type dockerMount struct {
	Type        string `json:"Type"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

// resolveContainerBindSources translates paths inside AgentAPI to paths on the
// Docker host. Only a single user's workspace and a single skill directory are
// passed to docker run; never inherit the API container's other mounts.
func (r *DockerSkillShellRuntime) resolveContainerBindSources(ctx context.Context, workspace, skillRoot string) (string, string, error) {
	if !sandboxPathWithin(workspace, r.config.WorkspaceRoot, false) {
		return "", "", fmt.Errorf("skill sandbox workspace is outside the configured user workspace root")
	}
	for _, forbidden := range []string{"/run/agentapi/secrets", "/var/run/docker.sock"} {
		if sandboxPathWithin(workspace, forbidden, true) || sandboxPathWithin(skillRoot, forbidden, true) {
			return "", "", fmt.Errorf("skill sandbox path overlaps a protected API mount")
		}
	}
	allowedSkillRoot := sandboxPathWithin(skillRoot, workspace, true)
	for _, root := range r.config.SkillRoots {
		if sandboxPathWithin(skillRoot, root, false) {
			allowedSkillRoot = true
			break
		}
	}
	if !allowedSkillRoot {
		return "", "", fmt.Errorf("skill sandbox root is not under a configured skill or task workspace")
	}
	out, err := exec.CommandContext(ctx, r.commandBin, "inspect", "--format", "{{json .Mounts}}", containerHostname()).Output()
	if err != nil {
		return "", "", fmt.Errorf("inspect API container sandbox mounts: %w", err)
	}
	var mounts []dockerMount
	if err := json.Unmarshal(out, &mounts); err != nil {
		return "", "", fmt.Errorf("decode API container sandbox mounts: %w", err)
	}
	workSource, err := sandboxHostBindSource(workspace, mounts)
	if err != nil {
		return "", "", err
	}
	skillSource, err := sandboxHostBindSource(skillRoot, mounts)
	if err != nil {
		return "", "", err
	}
	return workSource, skillSource, nil
}

func sandboxPathWithin(path, base string, allowBase bool) bool {
	if !filepath.IsAbs(path) || !filepath.IsAbs(base) {
		return false
	}
	rel, err := filepath.Rel(base, path)
	return err == nil && (rel == "." && allowBase || rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func sandboxHostBindSource(path string, mounts []dockerMount) (string, error) {
	var selected *dockerMount
	for i := range mounts {
		m := &mounts[i]
		if (m.Type != "bind" && m.Type != "volume") || !filepath.IsAbs(m.Source) || !sandboxPathWithin(path, m.Destination, true) {
			continue
		}
		if selected == nil || len(m.Destination) > len(selected.Destination) {
			selected = m
		}
	}
	if selected == nil {
		return "", fmt.Errorf("skill sandbox path %q has no explicit bindable mount", path)
	}
	if strings.ContainsAny(selected.Source, ",\n\r") || strings.ContainsAny(path, ",\n\r") {
		return "", fmt.Errorf("skill sandbox bind path contains unsupported delimiter")
	}
	rel, _ := filepath.Rel(selected.Destination, path)
	// Reject a symlink in any component: the host Docker daemon may resolve it
	// outside the intended mount even when the path looks like a subdirectory.
	component := selected.Destination
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		component = filepath.Join(component, part)
		info, err := os.Lstat(component)
		if err != nil {
			return "", fmt.Errorf("inspect skill sandbox path %q: %w", component, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("skill sandbox path %q contains symlink", component)
		}
	}
	return filepath.Join(selected.Source, rel), nil
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
	tmpDir := filepath.Join(containerWorkspaceDir, "tmp")
	args = append(args,
		"--mount", "type=bind,source="+workspace+",target="+containerWorkspaceDir,
		"--mount", "type=bind,source="+skillRoot+",target="+containerSkillDir+",readonly",
		"-w", containerSkillDir,
		"-e", "AGENT_WORKSPACE_DIR="+containerWorkspaceDir,
		"-e", "CLAUDE_SKILL_DIR="+containerSkillDir,
		"-e", "SKILL_DIR="+containerSkillDir,
		"-e", "TMP_DIR="+tmpDir,
		"-e", "TMPDIR="+tmpDir,
	)
	for _, key := range sortedMapKeys(r.env) {
		if skillShellReservedEnvKey(key) {
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

func skillShellReservedEnvKey(key string) bool {
	switch key {
	case "AGENT_WORKSPACE_DIR", "CLAUDE_SKILL_DIR", "SKILL_DIR", "TMP_DIR", "TMPDIR":
		return true
	default:
		return false
	}
}

func localSkillShellReservedEnvKey(key string) bool {
	switch key {
	case "AGENT_WORKSPACE_DIR", "CLAUDE_SKILL_DIR", "SKILL_DIR", "TMP_DIR", "TMPDIR", "PATH", "HOME", "PWD", "TERM", "LANG", "LC_ALL", "TMP":
		return true
	default:
		return false
	}
}

func localSkillShellEnvironment(workingDir, skillRoot string, env map[string]string) []string {
	workingDir = filepath.Clean(strings.TrimSpace(workingDir))
	if workingDir == "" {
		workingDir = "."
	}
	skillRoot = filepath.Clean(strings.TrimSpace(skillRoot))
	if skillRoot == "" {
		skillRoot = workingDir
	}
	workspaceDir := strings.TrimSpace(env["AGENT_WORKSPACE_DIR"])
	if workspaceDir == "" {
		workspaceDir = workingDir
	}
	declaredSkillRoot := strings.TrimSpace(env["CLAUDE_SKILL_DIR"])
	if declaredSkillRoot == "" {
		declaredSkillRoot = skillRoot
	}
	tmpDir := strings.TrimSpace(env["TMP_DIR"])
	if tmpDir == "" {
		tmpDir = filepath.Join(workspaceDir, "tmp")
	}
	values := map[string]string{
		"AGENT_WORKSPACE_DIR": workspaceDir,
		"CLAUDE_SKILL_DIR":    declaredSkillRoot,
		"SKILL_DIR":           declaredSkillRoot,
		"HOME":                workspaceDir,
		"PATH":                defaultSkillShellPATH,
		"PWD":                 workingDir,
		"TERM":                "dumb",
		"LANG":                "C.UTF-8",
		"LC_ALL":              "C.UTF-8",
		"TMP_DIR":             tmpDir,
		"TMPDIR":              tmpDir,
		"TMP":                 tmpDir,
	}
	for key, value := range env {
		if localSkillShellReservedEnvKey(key) {
			continue
		}
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
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
