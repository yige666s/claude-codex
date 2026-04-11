package websandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DockerExecutor struct {
	image          string
	timeout        time.Duration
	networkEnabled bool
	autoBuild      bool
	runner         CommandRunner
	outputBaseDir  string
	audit          AuditSink
}

func NewDockerExecutor(opts RuntimeOptions, audit AuditSink) *DockerExecutor {
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = defaultImage
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	outputBaseDir := opts.OutputBaseDir
	if strings.TrimSpace(outputBaseDir) == "" {
		outputBaseDir = filepath.Join(os.TempDir(), "claude-codex-websandbox")
	}
	runner := opts.Runner
	if runner == nil {
		runner = OSCommandRunner{}
	}
	if audit == nil {
		audit = NoopAuditSink{}
	}
	return &DockerExecutor{
		image:          image,
		timeout:        timeout,
		networkEnabled: opts.NetworkEnabled,
		autoBuild:      opts.AutoBuildImage,
		runner:         runner,
		outputBaseDir:  outputBaseDir,
		audit:          audit,
	}
}

func (d *DockerExecutor) Execute(ctx context.Context, scope Scope, action Action, lease Lease) (string, error) {
	if err := d.ensureImage(ctx); err != nil {
		return "", err
	}
	outputDir, err := d.prepareOutputDir(scope)
	if err != nil {
		return "", err
	}
	args := []string{
		"run", "--rm", "--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", "128",
		"--memory", "512m",
		"--cpus", "1.0",
		"--workdir", containerSkillRoot,
		"--mount", fmt.Sprintf("type=bind,src=%s,dst=%s,readonly", filepath.Clean(scope.RootDir), containerSkillRoot),
		"--mount", fmt.Sprintf("type=bind,src=%s,dst=%s", outputDir, containerOutputDir),
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
		"--env", "HOME=" + containerOutputDir,
		"--env", "CLAUDE_GO_EXECUTION_ID=" + lease.ID,
		"--env", "CLAUDE_GO_AGENT_ID=" + lease.AgentID,
	}
	if d.networkEnabled {
		args = append(args, "--network", "bridge")
	} else {
		args = append(args, "--network", "none")
	}
	for key, value := range lease.Env {
		args = append(args, "--env", key+"="+value)
	}
	if len(scope.AllowedDomains) > 0 {
		args = append(args, "--env", "CLAUDE_GO_ALLOWED_DOMAINS="+strings.Join(scope.AllowedDomains, ","))
	}
	args = append(args, d.image, action.Binary)
	args = append(args, action.Args...)

	d.audit.Record(AuditEvent{
		Timestamp: time.Now().UTC(),
		Event:     "docker_run",
		SessionID: scope.SessionID,
		SkillName: scope.SkillName,
		AgentID:   lease.AgentID,
		TaskID:    lease.TaskID,
		Command:   action.RawCommand,
		Action:    string(action.Type),
		Image:     d.image,
	})

	result, err := d.runner.Run(ctx, ExecRequest{
		Name:    "docker",
		Args:    args,
		Timeout: d.timeout,
	})
	if err != nil {
		stderr := strings.TrimSpace(rewriteOutputPaths(result.Stderr, outputDir))
		stdout := strings.TrimSpace(rewriteOutputPaths(result.Stdout, outputDir))
		d.audit.Record(AuditEvent{
			Timestamp: time.Now().UTC(),
			Event:     "docker_run_failed",
			SessionID: scope.SessionID,
			SkillName: scope.SkillName,
			AgentID:   lease.AgentID,
			TaskID:    lease.TaskID,
			Command:   action.RawCommand,
			Action:    string(action.Type),
			Image:     d.image,
			Metadata: map[string]string{
				"stdout": stdout,
				"stderr": stderr,
			},
		})
		if stderr == "" {
			stderr = stdout
		}
		return "", fmt.Errorf("docker sandbox execution failed: %w\n%s", err, stderr)
	}
	return strings.TrimSpace(rewriteOutputPaths(result.Stdout, outputDir)), nil
}

func (d *DockerExecutor) ensureImage(ctx context.Context) error {
	inspectResult, err := d.runner.Run(ctx, ExecRequest{
		Name:    "docker",
		Args:    []string{"image", "inspect", d.image},
		Timeout: 15 * time.Second,
	})
	if err == nil {
		return nil
	}
	if !d.autoBuild {
		return fmt.Errorf("docker sandbox image %q is not available: %s", d.image, coalesceDockerLogs(inspectResult))
	}
	tmpDir, mkErr := os.MkdirTemp("", "claude-codex-websandbox-build-*")
	if mkErr != nil {
		return mkErr
	}
	defer os.RemoveAll(tmpDir)
	if writeErr := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(defaultDockerfile), 0o644); writeErr != nil {
		return writeErr
	}
	buildResult, err := d.runner.Run(ctx, ExecRequest{
		Name:    "docker",
		Args:    []string{"build", "-t", d.image, tmpDir},
		Timeout: 10 * time.Minute,
	})
	if err != nil {
		logs := coalesceDockerLogs(buildResult)
		d.audit.Record(AuditEvent{
			Timestamp: time.Now().UTC(),
			Event:     "docker_build_failed",
			Image:     d.image,
			Metadata: map[string]string{
				"stdout": strings.TrimSpace(buildResult.Stdout),
				"stderr": strings.TrimSpace(buildResult.Stderr),
			},
		})
		if logs != "" {
			return fmt.Errorf("failed to build docker sandbox image %q: %w\n%s", d.image, err, logs)
		}
		return fmt.Errorf("failed to build docker sandbox image %q: %w", d.image, err)
	}
	return nil
}

func (d *DockerExecutor) prepareOutputDir(scope Scope) (string, error) {
	base := filepath.Join(d.outputBaseDir, sanitizePathPart(scope.SkillName), sanitizePathPart(scope.SessionID))
	if strings.TrimSpace(scope.SkillName) == "" {
		base = filepath.Join(d.outputBaseDir, "default", sanitizePathPart(scope.SessionID))
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	dir := filepath.Join(base, fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func sanitizePathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "session"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return replacer.Replace(value)
}

func rewriteOutputPaths(text, outputDir string) string {
	text = strings.ReplaceAll(text, containerOutputDir, outputDir)
	return text
}

func coalesceDockerLogs(result ExecResult) string {
	stderr := strings.TrimSpace(result.Stderr)
	stdout := strings.TrimSpace(result.Stdout)
	switch {
	case stderr != "" && stdout != "":
		return stderr + "\n" + stdout
	case stderr != "":
		return stderr
	default:
		return stdout
	}
}

const defaultDockerfile = `
FROM python:3.11-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends bash ca-certificates findutils coreutils \
    && rm -rf /var/lib/apt/lists/*
RUN python3 -m pip install --no-cache-dir requests
RUN useradd -m -u 1000 sandbox
USER sandbox
WORKDIR /workspace/skill
`
