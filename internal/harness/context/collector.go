package context

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// WorkspaceContext holds collected workspace information
type WorkspaceContext struct {
	WorkingDir   string
	GitBranch    string
	GitStatus    string
	ClaudeMD     string
	DirectoryMap string
	Platform     string
	OSVersion    string
	Shell        string
	IsGitRepo    bool
}

// CollectorOptions configures context collection behavior
type CollectorOptions struct {
	IncludeGit          bool
	IncludeClaudeMd     bool
	IncludeDirectoryMap bool
	DirectoryDepth      int
	MaxStatusChars      int
}

// DefaultCollectorOptions returns default collector options
func DefaultCollectorOptions() *CollectorOptions {
	return &CollectorOptions{
		IncludeGit:          true,
		IncludeClaudeMd:     true,
		IncludeDirectoryMap: false,
		DirectoryDepth:      2,
		MaxStatusChars:      MaxStatusChars,
	}
}

// Collect gathers workspace context from the given directory
func Collect(workDir string) WorkspaceContext {
	return CollectWithOptions(workDir, DefaultCollectorOptions())
}

// CollectWithOptions gathers workspace context with custom options
func CollectWithOptions(workDir string, opts *CollectorOptions) WorkspaceContext {
	ctx := WorkspaceContext{
		WorkingDir: workDir,
		Platform:   detectPlatform(),
		OSVersion:  detectOSVersion(),
		Shell:      detectShell(),
		IsGitRepo:  isGitRepository(workDir),
	}

	if opts.IncludeGit && ctx.IsGitRepo {
		ctx.GitBranch = gitBranch(workDir)
		ctx.GitStatus = gitStatus(workDir)
	}

	if opts.IncludeClaudeMd {
		ctx.ClaudeMD = readClaudeMD(workDir)
	}

	if opts.IncludeDirectoryMap {
		ctx.DirectoryMap = buildDirectoryMap(workDir, opts.DirectoryDepth)
	}

	return ctx
}

// SystemPrompt formats the context as a system prompt prefix
func (w WorkspaceContext) SystemPrompt() string {
	var sb strings.Builder
	sb.WriteString("<system>\n")
	sb.WriteString("Working directory: " + w.WorkingDir + "\n")
	sb.WriteString("Platform: " + w.Platform + "\n")
	if w.GitBranch != "" {
		sb.WriteString("Git branch: " + w.GitBranch + "\n")
	}
	if w.GitStatus != "" {
		sb.WriteString("Git status:\n" + w.GitStatus + "\n")
	}
	if w.DirectoryMap != "" {
		sb.WriteString("Directory structure:\n" + w.DirectoryMap + "\n")
	}
	if w.ClaudeMD != "" {
		sb.WriteString("Project instructions (CLAUDE.md):\n" + w.ClaudeMD + "\n")
	}
	sb.WriteString("</system>")
	return sb.String()
}

func gitBranch(dir string) string {
	out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func gitStatus(dir string) string {
	out, err := runGit(dir, "status", "--short")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func readClaudeMD(dir string) string {
	for _, name := range []string{"CLAUDE.md", "claude.md", ".claude/CLAUDE.md"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			return string(data)
		}
	}
	return ""
}

func buildDirectoryMap(dir string, depth int) string {
	var sb strings.Builder
	walkDir(&sb, dir, "", depth)
	return sb.String()
}

func walkDir(sb *strings.Builder, dir, prefix string, depth int) {
	if depth < 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
			continue
		}
		sb.WriteString(prefix + name)
		if e.IsDir() {
			sb.WriteString("/\n")
			walkDir(sb, filepath.Join(dir, name), prefix+"  ", depth-1)
		} else {
			sb.WriteString("\n")
		}
	}
}

func detectPlatform() string {
	return runtime.GOOS
}

func detectOSVersion() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("uname", "-r").Output()
		if err == nil {
			return "Darwin " + strings.TrimSpace(string(out))
		}
	case "linux":
		out, err := exec.Command("uname", "-r").Output()
		if err == nil {
			return "Linux " + strings.TrimSpace(string(out))
		}
	case "windows":
		out, err := exec.Command("cmd", "/c", "ver").Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	if shell != "" {
		return filepath.Base(shell)
	}
	return ""
}

func isGitRepository(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// ToMap converts workspace context to a map
func (w WorkspaceContext) ToMap() map[string]string {
	result := make(map[string]string)

	result["workingDir"] = w.WorkingDir
	result["platform"] = w.Platform

	if w.OSVersion != "" {
		result["osVersion"] = w.OSVersion
	}

	if w.Shell != "" {
		result["shell"] = w.Shell
	}

	if w.GitBranch != "" {
		result["gitBranch"] = w.GitBranch
	}

	if w.GitStatus != "" {
		result["gitStatus"] = w.GitStatus
	}

	if w.ClaudeMD != "" {
		result["claudeMd"] = w.ClaudeMD
	}

	if w.DirectoryMap != "" {
		result["directoryMap"] = w.DirectoryMap
	}

	return result
}
