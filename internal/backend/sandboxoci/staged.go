package sandboxoci

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"claude-codex/internal/backend/sandboxpolicy"
	"claude-codex/internal/backend/sandboxtransfer"
)

// StagedMountPlanner validates Node-owned cache and task paths for a future
// bounded OCI driver. It cannot run a container: writable host bind mounts
// lack a hard disk quota and restart-safe orphan reconciliation at present.
type StagedMountPlanner struct {
	SkillCacheRoot string
	TaskRoot       string
}

func (p StagedMountPlanner) Plan(profile sandboxpolicy.Profile, skillPath, workspacePath string) ([]string, error) {
	if err := sandboxpolicy.ValidateFileOCIProfile(profile); err != nil {
		return nil, err
	}
	skill, err := validatedBind(p.SkillCacheRoot, skillPath, "skill")
	if err != nil {
		return nil, err
	}
	work, err := validatedBind(p.TaskRoot, workspacePath, "workspace")
	if err != nil {
		return nil, err
	}
	return stagedRunArgs(profile, "agentapi-oci-planned", skill, work), nil
}
func validatedBind(base, target, kind string) (string, error) {
	if base == "" || target == "" || !filepath.IsAbs(base) || !filepath.IsAbs(target) || strings.ContainsAny(base+target, ",\n\r\x00") {
		return "", errors.New("invalid sandbox bind source")
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	parent, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(parent, absolute)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("sandbox bind outside private root")
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if kind == "skill" {
		if len(parts) != 2 || parts[1] != "skill" || len(parts[0]) != 64 {
			return "", errors.New("invalid cached skill bind")
		}
		for _, c := range parts[0] {
			if !((c >= 'a' && c <= 'f') || (c >= '0' && c <= '9')) {
				return "", errors.New("invalid cached skill digest")
			}
		}
	} else if kind == "workspace" {
		if len(parts) != 3 || !strings.HasPrefix(parts[0], "task-") || parts[1] != "inputs" || parts[2] != "workspace" {
			return "", errors.New("invalid task workspace bind")
		}
	} else {
		return "", errors.New("invalid bind kind")
	}
	actualParent, err := filepath.EvalSymlinks(parent)
	if err != nil || actualParent != parent {
		return "", errors.New("sandbox bind base has symlinks")
	}
	for path := absolute; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("sandbox bind path must be real directories")
		}
		if path == parent {
			break
		}
		if path == filepath.Dir(path) {
			return "", errors.New("sandbox bind root not found")
		}
	}
	return absolute, nil
}
func stagedRunArgs(p sandboxpolicy.Profile, name, skill, workspace string) []string {
	return []string{
		"run", "--rm", "-i", "--name", name, "--label", "agentapi.sandbox_task=true",
		"--pull=never", "--network=none", "--read-only", "--cap-drop=ALL",
		"--security-opt=no-new-privileges", "--cgroupns=private",
		"--memory", strconv.Itoa(p.MemoryMiB) + "m", "--memory-swap", strconv.Itoa(p.MemoryMiB) + "m",
		"--cpus", strconv.FormatFloat(p.CPUs, 'f', -1, 64), "--pids-limit", strconv.Itoa(p.PIDs),
		"--mount", "type=bind,source=" + skill + ",target=/skill,readonly,bind-propagation=rprivate",
		"--mount", "type=bind,source=" + workspace + ",target=/input,readonly,bind-propagation=rprivate",
		"--tmpfs", "/workspace:rw,nosuid,nodev,size=" + strconv.Itoa(p.ScratchMiB) + "m,uid=65534,gid=65534,mode=0700",
		"--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=32m,mode=1777",
		"--user", "65534:65534", "--workdir", "/workspace", "--env", "HOME=/workspace",
		"--entrypoint", "/bin/sh", p.ImageDigest, "-s",
	}
}

// CopyInputToScratch is a fixed prologue to be run before any user command.
// No caller-supplied path is interpolated into the shell text. Input archives
// reject links; the initial copy consumes only the bounded /workspace tmpfs.
const CopyInputToScratch = "set -eu\ncp -R /input/. /workspace/\nchmod -R u+w /workspace\nmkdir -p /workspace/outputs\n"

// CollectStagedOutputs validates a bounded archive streamed from a *live*
// container, e.g. via docker exec tar. Docker cp cannot export tmpfs mounts.
// It does not itself run Docker, persist artifacts or expose a Node route.
func CollectStagedOutputs(ctx context.Context, stream io.Reader, profile sandboxpolicy.Profile) ([]sandboxtransfer.OutputFile, error) {
	if err := sandboxpolicy.ValidateFileOCIProfile(profile); err != nil {
		return nil, err
	}
	return sandboxtransfer.CollectOutputs(ctx, stream, int64(profile.MaxArtifactBytes), sandboxtransfer.HardMaxFiles)
}
