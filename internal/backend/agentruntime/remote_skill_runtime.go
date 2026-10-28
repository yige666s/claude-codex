package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"claude-codex/internal/harness/skills"
)

// RemoteSkillRequest is local Broker input, not an HTTP payload. Workspace
// paths are used to build an approved snapshot; never send host paths directly
// to the Node or grant them as Docker bind mounts.
type RemoteSkillRequest struct {
	UserID     string
	SessionID  string
	JobID      string
	SkillName  string
	WorkingDir string
	SkillRoot  string
	Command    string
}

// RemoteSkillPreparer pins an immutable published Skill before prompt shell
// blocks run. The returned context must be used for the full Skill invocation.
// No prepared context means no remote command may execute.
type RemoteSkillPreparer interface {
	PrepareSkill(context.Context, RemoteSkillRequest) (context.Context, []byte, func() error, error)
}

type RemoteSkillArtifact struct {
	Filename string
	Content  []byte
}
type RemoteSkillCollector interface {
	CollectSkillArtifacts(context.Context, RemoteSkillRequest) ([]RemoteSkillArtifact, error)
}

type RemoteSkillExecutor interface {
	ExecuteSkillCommand(context.Context, RemoteSkillRequest) (string, error)
}

type RemoteSkillShellRuntime struct {
	executor  RemoteSkillExecutor
	parent    context.Context
	request   RemoteSkillRequest
	shell     skills.FrontmatterShell
	allowed   []string
	maxOutput int
}

func NewRemoteSkillShellRuntime(executor RemoteSkillExecutor, parent context.Context, request RemoteSkillRequest, shell skills.FrontmatterShell, allowed []string, maxOutput int) *RemoteSkillShellRuntime {
	if maxOutput <= 0 || maxOutput > 1<<20 {
		maxOutput = 1 << 20
	}
	return &RemoteSkillShellRuntime{executor: executor, parent: parent, request: request, shell: shell, allowed: append([]string(nil), allowed...), maxOutput: maxOutput}
}
func (r *RemoteSkillShellRuntime) ValidateCommand(command string) error {
	if r == nil || r.executor == nil {
		return errors.New("remote skill executor is not configured; refusing local fallback")
	}
	if r.request.UserID == "" || r.request.SessionID == "" {
		return errors.New("remote skill execution requires a user and session")
	}
	if len(command) > 64<<10 {
		return errors.New("remote skill command exceeds size limit")
	}
	if strings.TrimSpace(command) == "" {
		return errors.New("remote skill command is empty")
	}
	return skills.ValidatePromptShellCommand(command, r.shell, r.allowed)
}
func (r *RemoteSkillShellRuntime) ExecuteCommand(ctx context.Context, command string) (string, error) {
	if err := r.ValidateCommand(command); err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r.parent != nil {
		if err := r.parent.Err(); err != nil {
			return "", err
		}
		combined, cancel := context.WithCancel(r.parent)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		if deadline, ok := ctx.Deadline(); ok {
			var deadlineCancel context.CancelFunc
			combined, deadlineCancel = context.WithDeadline(combined, deadline)
			defer deadlineCancel()
		}
		ctx = combined
	}
	req := r.request
	req.Command = command
	if req.JobID == "" {
		req.JobID = jobIDFromContext(ctx)
	}
	output, err := r.executor.ExecuteSkillCommand(ctx, req)
	if err != nil {
		return "", fmt.Errorf("remote isolated skill execution failed: %w", err)
	}
	if r.maxOutput > 0 && len(output) > r.maxOutput {
		return "", errors.New("remote skill output exceeds configured limit")
	}
	return output, nil
}

// validatePublishedSkillDefinition prevents a cached in-memory prompt and its
// permissions from diverging from the entrypoint pinned by the Broker.
func validatePublishedSkillDefinition(skill *skills.SkillDefinition, published []byte) error {
	if skill == nil || skill.LoadedFrom != string(skills.LoadedFromSkills) {
		return errors.New("remote Skill must be loaded from a published skill directory")
	}
	parsed, err := skills.ParseSkillFile(string(published))
	if err != nil || parsed == nil || parsed.Frontmatter == nil {
		return errors.New("published Skill entrypoint invalid")
	}
	fm := parsed.Frontmatter
	allowedEnv, primaryEnv := skills.ParseSkillMetadataEnv(fm.Metadata)
	if parsed.Content != skill.Content ||
		!reflect.DeepEqual(skills.ParseAllowedTools(fm.AllowedTools), skill.AllowedTools) ||
		!reflect.DeepEqual(skills.ParseArgumentNames(fm.Arguments), skill.ArgumentNames) ||
		!reflect.DeepEqual(skills.ParseSkillMetadata(fm.Metadata), skill.Metadata) ||
		!reflect.DeepEqual(allowedEnv, skill.AllowedEnv) || primaryEnv != skill.PrimaryEnv ||
		!reflect.DeepEqual(fm.Hooks, skill.Hooks) ||
		skills.ParseSkillMetadataRunAsJob(fm.Metadata) != skill.RunAsJob ||
		skills.ParseShellFrontmatter(fm.Shell) != skill.Shell || fm.Version != skill.Version {
		return errors.New("loaded Skill prompt or metadata differs from published release")
	}
	return nil
}

// RemoteSkillRouter gates canary per user; non-canary traffic retains the
// existing configured runner rather than a global remote override.
type RemoteSkillRouter interface {
	RemoteSkillEnabled(context.Context, string, string) bool
}
