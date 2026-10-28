package sandboxbroker

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/backend/sandboxartifact"
	"claude-codex/internal/backend/sandboxpolicy"
	"claude-codex/internal/backend/sandboxtransfer"
)

// ExecutionEnvelope is sent only by a trusted Broker client. It intentionally
// contains no host mount sources, credentials, native trust assertion or Docker args.
type ExecutionEnvelope struct {
	TaskID         string                `json:"task_id"`
	ParentJobID    string                `json:"parent_job_id,omitempty"`
	UserID         string                `json:"user_id"`
	SessionID      string                `json:"session_id"`
	SkillName      string                `json:"skill_name"`
	SkillRef       sandboxartifact.Ref   `json:"skill_ref"`
	PolicyRevision string                `json:"policy_revision"`
	Profile        sandboxpolicy.Profile `json:"profile"`
	Deadline       time.Time             `json:"deadline"`
	Snapshot       []byte                `json:"snapshot,omitempty"`
	LeaseID        string                `json:"lease_id,omitempty"`
	ExecutionID    string                `json:"execution_id"`
	Command        string                `json:"command"`
}

// SkillCatalog resolves a prepublished, immutable release; it must not
// publish or package a Skill on the request path. Origin trust is separate.
type SkillCatalog interface {
	Resolve(context.Context, agentruntime.RemoteSkillRequest) (sandboxartifact.Ref, error)
}

type NodeClient interface {
	PrepareIsolated(context.Context, string, string, ExecutionEnvelope) (string, error)
	ExecuteIsolated(context.Context, string, string, ExecutionEnvelope) (string, error)
	DisposeIsolated(context.Context, string, string, ExecutionEnvelope) error
	CollectIsolated(context.Context, string, string, ExecutionEnvelope) ([]sandboxtransfer.OutputFile, error)
}

type Executor struct {
	Policy            sandboxpolicy.ActiveReader
	ResolveCredential sandboxpolicy.CredentialResolver
	Snapshot          sandboxtransfer.Builder
	SkillCatalog      SkillCatalog
	Artifacts         sandboxartifact.Store
	Node              NodeClient
	AllowedUserIDs    map[string]bool
}

type preparedKey struct{}
type preparedTask struct {
	request        agentruntime.RemoteSkillRequest
	ref            sandboxartifact.Ref
	taskID         string
	policyRevision int64
	profile        sandboxpolicy.Profile
	pool           sandboxpolicy.NodePool
	leaseID        string
	finished       atomic.Bool
}

func (b *Executor) route(ctx context.Context, req agentruntime.RemoteSkillRequest) (sandboxpolicy.Revision, sandboxpolicy.Profile, sandboxpolicy.NodePool, error) {
	if b == nil || b.Policy == nil || b.ResolveCredential == nil || b.Node == nil || b.SkillCatalog == nil || !b.AllowedUserIDs[req.UserID] {
		return sandboxpolicy.Revision{}, sandboxpolicy.Profile{}, sandboxpolicy.NodePool{}, errors.New("remote sandbox not enabled for this user")
	}
	if ctx == nil || ctx.Err() != nil || req.UserID == "" || req.SessionID == "" || req.SkillName == "" || req.SkillRoot == "" || req.WorkingDir == "" {
		return sandboxpolicy.Revision{}, sandboxpolicy.Profile{}, sandboxpolicy.NodePool{}, errors.New("invalid remote sandbox request")
	}
	if runtime.GOOS != "linux" {
		return sandboxpolicy.Revision{}, sandboxpolicy.Profile{}, sandboxpolicy.NodePool{}, errors.New("remote sandbox control plane requires Linux")
	}
	active, err := b.Policy.Active(ctx)
	if err != nil {
		return sandboxpolicy.Revision{}, sandboxpolicy.Profile{}, sandboxpolicy.NodePool{}, errors.New("sandbox policy unavailable")
	}
	if err = active.Document.Validate(); err != nil || !active.Document.Enabled {
		return sandboxpolicy.Revision{}, sandboxpolicy.Profile{}, sandboxpolicy.NodePool{}, errors.New("sandbox policy disabled or invalid")
	}
	profileID, ok := active.Document.ProfileFor("unknown")
	if !ok {
		return sandboxpolicy.Revision{}, sandboxpolicy.Profile{}, sandboxpolicy.NodePool{}, errors.New("no isolated route")
	}
	profile := active.Document.Profiles[profileID]
	if err = sandboxpolicy.ValidateFileOCIProfile(profile); err != nil {
		return sandboxpolicy.Revision{}, sandboxpolicy.Profile{}, sandboxpolicy.NodePool{}, errors.New("invalid isolated profile")
	}
	pool := active.Document.NodePools[profile.NodePool]
	if pool.Environment != "development" || pool.MaxConcurrency != 1 {
		return sandboxpolicy.Revision{}, sandboxpolicy.Profile{}, sandboxpolicy.NodePool{}, errors.New("remote sandbox requires a single-slot development pool")
	}
	return active, profile, pool, nil
}

// PrepareSkill is called before prompt expansion. It fixes the published
// version and a workspace snapshot in one invocation-local context. It also
// rejects stale in-memory prompt definitions: the live SKILL.md and the
// published entrypoint must match before any !-block can execute.
func (b *Executor) PrepareSkill(ctx context.Context, req agentruntime.RemoteSkillRequest) (context.Context, []byte, func() error, error) {
	active, profile, pool, err := b.route(ctx, req)
	if err != nil {
		return nil, nil, nil, err
	}
	ref, err := b.SkillCatalog.Resolve(ctx, req)
	if err != nil || ref.Validate() != nil || ref.ID != req.SkillName {
		return nil, nil, nil, errors.New("published skill unavailable")
	}
	published, err := b.Artifacts.SkillFile(ref)
	if err != nil {
		return nil, nil, nil, errors.New("published skill content unavailable")
	}
	live, err := readLoadedSkillEntrypoint(req.SkillRoot)
	if err != nil || !bytes.Equal(live, published) {
		return nil, nil, nil, errors.New("loaded and published skill versions differ")
	}
	snapshot, err := b.Snapshot.BuildWorkspace(ctx, req.WorkingDir)
	if err != nil {
		return nil, nil, nil, errors.New("sandbox input snapshot rejected")
	}
	taskID, err := newIdentity()
	if err != nil {
		return nil, nil, nil, err
	}
	secret, err := b.ResolveCredential(ctx, pool.CredentialRef)
	if err != nil || len(secret) < 32 {
		return nil, nil, nil, errors.New("sandbox node credential unavailable")
	}
	deadline := time.Now().Add(time.Duration(profile.TimeoutSeconds) * time.Second)
	if configured, ok := ctx.Deadline(); ok && configured.Before(deadline) {
		deadline = configured
	}
	if !deadline.After(time.Now()) {
		return nil, nil, nil, context.DeadlineExceeded
	}
	prepare := ExecutionEnvelope{TaskID: taskID, ParentJobID: req.JobID, UserID: req.UserID, SessionID: req.SessionID,
		SkillName: req.SkillName, SkillRef: ref, PolicyRevision: strconv.FormatInt(active.ID, 10),
		Profile: profile, Deadline: deadline, Snapshot: snapshot}
	leaseID, err := b.Node.PrepareIsolated(ctx, pool.Endpoint, secret, prepare)
	if err != nil || leaseID == "" {
		return nil, nil, nil, errors.New("sandbox node prepare failed")
	}
	req.Command = ""
	task := &preparedTask{request: req, ref: ref, taskID: taskID, policyRevision: active.ID, profile: profile, pool: pool, leaseID: leaseID}
	release := func() error {
		if task.finished.Swap(true) {
			return nil
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := b.Node.DisposeIsolated(cleanupCtx, pool.Endpoint, secret, ExecutionEnvelope{TaskID: taskID, LeaseID: leaseID, UserID: req.UserID}); err != nil {
			return errors.New("remote sandbox lease cleanup failed")
		}
		return nil
	}
	return context.WithValue(ctx, preparedKey{}, task), published, release, nil
}

func (b *Executor) RemoteSkillEnabled(ctx context.Context, userID string) bool {
	if b == nil || !b.AllowedUserIDs[userID] || b.Policy == nil || ctx == nil {
		return false
	}
	revision, err := b.Policy.Active(ctx)
	return err == nil && revision.Document.Enabled && revision.Document.Validate() == nil
}

// ExecuteSkillCommand never resolves current or repackages the Skill/workspace
// on its hot path. A command without a matching prepared context fails closed.
// CollectSkillArtifacts fetches the fixed, bounded output set before the
// prepared task is released. It refuses unprepared or changed identities.
func (b *Executor) CollectSkillArtifacts(ctx context.Context, req agentruntime.RemoteSkillRequest) ([]agentruntime.RemoteSkillArtifact, error) {
	active, profile, pool, err := b.route(ctx, req)
	if err != nil {
		return nil, err
	}
	task, ok := ctx.Value(preparedKey{}).(*preparedTask)
	if !ok || task == nil || task.finished.Load() || task.request.UserID != req.UserID || task.request.SessionID != req.SessionID || task.request.SkillName != req.SkillName || task.policyRevision != active.ID || task.profile != profile || task.pool != pool {
		return nil, errors.New("remote task not prepared for collection")
	}
	secret, err := b.ResolveCredential(ctx, pool.CredentialRef)
	if err != nil || len(secret) < 32 {
		return nil, errors.New("sandbox node credential unavailable")
	}
	files, err := b.Node.CollectIsolated(ctx, pool.Endpoint, secret, ExecutionEnvelope{LeaseID: task.leaseID, UserID: req.UserID})
	if err != nil {
		return nil, errors.New("remote sandbox collection failed")
	}
	total := 0
	artifacts := make([]agentruntime.RemoteSkillArtifact, 0, len(files))
	for _, file := range files {
		total += len(file.Content)
		hash := sha256.Sum256(file.Content)
		if total > profile.MaxArtifactBytes || file.Path == "" || filepath.Base(file.Path) != file.Path || file.SHA256 != hex.EncodeToString(hash[:]) {
			return nil, errors.New("remote sandbox artifact rejected")
		}
		artifacts = append(artifacts, agentruntime.RemoteSkillArtifact{Filename: file.Path, Content: file.Content})
	}
	return artifacts, nil
}

func (b *Executor) ExecuteSkillCommand(ctx context.Context, req agentruntime.RemoteSkillRequest) (string, error) {
	active, profile, pool, err := b.route(ctx, req)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(req.Command) == "" || len(req.Command) > 64<<10 {
		return "", errors.New("invalid remote sandbox command")
	}
	task, ok := ctx.Value(preparedKey{}).(*preparedTask)
	if !ok || task == nil || task.finished.Load() || task.request.UserID != req.UserID || task.request.SessionID != req.SessionID || task.request.SkillName != req.SkillName || task.request.WorkingDir != req.WorkingDir || task.request.SkillRoot != req.SkillRoot || task.policyRevision != active.ID || task.profile != profile || task.pool != pool {
		return "", errors.New("remote sandbox task not prepared or policy changed")
	}
	if task.request.JobID != "" && req.JobID != "" && task.request.JobID != req.JobID {
		return "", errors.New("remote sandbox job changed")
	}
	secret, err := b.ResolveCredential(ctx, pool.CredentialRef)
	if err != nil || len(secret) < 32 {
		return "", errors.New("sandbox node credential unavailable")
	}
	executionID, err := newIdentity()
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(time.Duration(profile.TimeoutSeconds) * time.Second)
	if configured, ok := ctx.Deadline(); ok && configured.Before(deadline) {
		deadline = configured
	}
	if !deadline.After(time.Now()) {
		return "", context.DeadlineExceeded
	}
	envelope := ExecutionEnvelope{
		TaskID: task.taskID, ExecutionID: executionID, ParentJobID: req.JobID, UserID: req.UserID, SessionID: req.SessionID,
		SkillName: req.SkillName, SkillRef: task.ref, PolicyRevision: strconv.FormatInt(active.ID, 10),
		Profile: profile, Deadline: deadline, LeaseID: task.leaseID,
		Command: rewriteCommandPaths(req.Command, req.WorkingDir, req.SkillRoot),
	}
	output, err := b.Node.ExecuteIsolated(ctx, pool.Endpoint, secret, envelope)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("remote sandbox execution failed")
	}
	if len(output) > profile.MaxOutputBytes {
		return "", errors.New("remote sandbox output exceeds profile limit")
	}
	return output, nil
}
func newIdentity() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", errors.New("sandbox identity unavailable")
	}
	return hex.EncodeToString(random[:]), nil
}

func rewriteCommandPaths(command, workspace, skillRoot string) string {
	out := command
	for _, entry := range []struct{ host, container string }{{skillRoot, "/skill"}, {workspace, "/workspace"}} {
		if entry.host == "" {
			continue
		}
		clean := filepath.Clean(entry.host)
		out = strings.ReplaceAll(out, clean, entry.container)
		out = strings.ReplaceAll(out, filepath.ToSlash(clean), entry.container)
	}
	return out
}

// Bound the only live read at invocation start. os.Root confines resolution to
// the configured Skill directory even if SKILL.md is concurrently replaced.
func readLoadedSkillEntrypoint(skillRoot string) ([]byte, error) {
	root, err := os.OpenRoot(skillRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open("SKILL.md")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > sandboxtransfer.HardMaxFileBytes {
		return nil, errors.New("invalid loaded Skill entrypoint")
	}
	content, err := io.ReadAll(io.LimitReader(file, sandboxtransfer.HardMaxFileBytes+1))
	if err != nil || len(content) > sandboxtransfer.HardMaxFileBytes {
		return nil, errors.New("loaded Skill entrypoint exceeds limit")
	}
	return content, nil
}

var _ agentruntime.RemoteSkillExecutor = (*Executor)(nil)
var _ agentruntime.RemoteSkillPreparer = (*Executor)(nil)
