package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	publictypes "claude-codex/internal/public/types"
)

const (
	memoryInjectedKey           = "agentruntime.memory_context_injected"
	consumerSecurityInjectedKey = "agentruntime.consumer_security_context_injected"
)

const consumerSecuritySystemContext = `<consumer-security>
You are serving a consumer web user. Do not expose internal server tools, tool names, file paths, workspace paths, shell commands, environment variables, credentials, stack traces, or raw provider errors.

Never claim that you can read local files, list project files, search server file contents, create arbitrary files, edit files, run shell commands, or inspect the server filesystem for the user. These are internal infrastructure capabilities, not user-facing product features.

If the user asks for local filesystem access, source-code search, arbitrary file creation/editing, shell execution, secrets, env vars, or server paths, politely refuse and offer safe alternatives: ask them to upload the file, use a published user-facing skill, or generate an artifact only through an approved skill flow.

Only describe published product skills and user-visible artifact/attachment flows. Do not mention hidden tools or implementation details.
</consumer-security>`

var ErrSessionNotRunning = errors.New("session is not running")
var ErrRuntimeShuttingDown = errors.New("runtime is shutting down")

type Runtime struct {
	config          RuntimeConfig
	sessions        SessionStore
	memory          MemoryService
	memoryExtract   MemoryExtractor
	memoryAbstract  MemoryAbstractor
	memoryOrganizer MemoryOrganizer
	artifacts       *ArtifactService
	jobs            JobStore
	jobEvents       *jobEventBroker
	skills          SkillCatalog
	skillExecutions SkillExecutionStore
	engineFactory   EngineFactory
	riskScanner     RiskScanner
	riskRecorder    func(context.Context, RiskEvent)

	mu           sync.Mutex
	wg           sync.WaitGroup
	running      map[string]context.CancelFunc
	runningJobs  map[string]context.CancelFunc
	shuttingDown bool
}

func (r *Runtime) SetArtifactService(artifacts *ArtifactService) {
	r.artifacts = artifacts
}

func (r *Runtime) SetJobStore(jobs JobStore) {
	r.jobs = jobs
}

func (r *Runtime) SetSkillExecutionStore(store SkillExecutionStore) {
	r.skillExecutions = store
}

func (r *Runtime) SetRiskScanner(scanner RiskScanner) {
	r.riskScanner = scanner
}

func (r *Runtime) SetRiskRecorder(recorder func(context.Context, RiskEvent)) {
	r.riskRecorder = recorder
}

func (r *Runtime) MaxAssetBytes() int64 {
	if r == nil || r.artifacts == nil {
		return DefaultMaxAssetBytes
	}
	return r.artifacts.MaxBytes()
}

func NewRuntime(config RuntimeConfig, sessions SessionStore, memory MemoryService, skills SkillCatalog, engineFactory EngineFactory) *Runtime {
	if config.TurnTimeout <= 0 {
		config.TurnTimeout = 2 * time.Minute
	}
	if config.SkillShellTimeout <= 0 {
		config.SkillShellTimeout = 90 * time.Second
	}
	config.SkillShellSandbox = config.SkillShellSandbox.normalized()
	return &Runtime{
		config:          config,
		sessions:        sessions,
		memory:          memory,
		memoryExtract:   NewRuleMemoryExtractor(),
		memoryAbstract:  NewRuleMemoryAbstractor(),
		memoryOrganizer: NewRuleMemoryOrganizer(),
		skills:          skills,
		engineFactory:   engineFactory,
		running:         make(map[string]context.CancelFunc),
		runningJobs:     make(map[string]context.CancelFunc),
		jobEvents:       newJobEventBroker(128),
	}
}

func (r *Runtime) SetMemoryExtractor(extractor MemoryExtractor) {
	if extractor != nil {
		r.memoryExtract = extractor
	}
}

func (r *Runtime) SetMemoryAbstractor(abstractor MemoryAbstractor) {
	if abstractor != nil {
		r.memoryAbstract = abstractor
	}
}

func (r *Runtime) SetMemoryOrganizer(organizer MemoryOrganizer) {
	if organizer != nil {
		r.memoryOrganizer = organizer
	}
}

func (r *Runtime) CreateSession(ctx context.Context, userID, workingDir string) (*state.Session, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if r.config.UserWorkspaceRoot != "" {
		workingDir = r.userWorkspace(userID)
		if err := os.MkdirAll(workingDir, 0o755); err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(workingDir) == "" || !r.config.AllowCustomWorkingDir {
		workingDir = r.config.DefaultWorkingDir
	}
	workingDir = filepath.Clean(workingDir)
	if r.sessions == nil {
		return nil, fmt.Errorf("session store is required")
	}
	return r.sessions.Create(ctx, userID, workingDir)
}

func (r *Runtime) ListSessions(ctx context.Context, userID string) ([]*state.Session, error) {
	if r.sessions == nil {
		return nil, fmt.Errorf("session store is required")
	}
	return r.sessions.List(ctx, userID)
}

func (r *Runtime) GetSession(ctx context.Context, userID, sessionID string) (*state.Session, error) {
	if r.sessions == nil {
		return nil, fmt.Errorf("session store is required")
	}
	return r.sessions.Get(ctx, userID, sessionID)
}

func (r *Runtime) DeleteSession(ctx context.Context, userID, sessionID string) error {
	if r.sessions == nil {
		return fmt.Errorf("session store is required")
	}
	r.Cancel(userID, sessionID)
	if r.memory != nil {
		if err := r.memory.DeleteSession(ctx, userID, sessionID); err != nil {
			return err
		}
	}
	if r.artifacts != nil {
		if err := r.artifacts.DeleteSession(ctx, userID, sessionID); err != nil {
			return err
		}
	}
	if r.jobs != nil {
		if err := r.jobs.DeleteSession(ctx, userID, sessionID); err != nil {
			return err
		}
	}
	return r.sessions.Delete(ctx, userID, sessionID)
}

func (r *Runtime) DeleteSessionMemory(ctx context.Context, userID, sessionID string) error {
	if r.memory == nil {
		return nil
	}
	return r.memory.DeleteSession(ctx, userID, sessionID)
}

func (r *Runtime) DeleteUserMemory(ctx context.Context, userID string) error {
	if r.memory == nil {
		return nil
	}
	return r.memory.DeleteUser(ctx, userID)
}

func (r *Runtime) ListMemoryItems(ctx context.Context, userID string, filter MemoryItemFilter) ([]MemoryItem, error) {
	if r.memory == nil {
		return []MemoryItem{}, nil
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return []MemoryItem{}, nil
	}
	return service.ListMemoryItems(ctx, userID, filter)
}

func (r *Runtime) GetMemorySettings(ctx context.Context, userID string) (MemorySettings, error) {
	if r.memory == nil {
		return defaultMemorySettings(), nil
	}
	service, ok := r.memory.(MemorySettingsService)
	if !ok {
		return defaultMemorySettings(), nil
	}
	return service.GetMemorySettings(ctx, userID)
}

func (r *Runtime) UpdateMemorySettings(ctx context.Context, userID string, settings MemorySettings) (MemorySettings, error) {
	if r.memory == nil {
		return MemorySettings{}, fmt.Errorf("memory is not configured")
	}
	service, ok := r.memory.(MemorySettingsService)
	if !ok {
		return MemorySettings{}, fmt.Errorf("memory settings are not supported")
	}
	settings.UpdatedAt = time.Now().UTC()
	return service.UpdateMemorySettings(ctx, userID, settings)
}

func (r *Runtime) GetMemoryItem(ctx context.Context, userID, itemID string) (MemoryItem, error) {
	if r.memory == nil {
		return MemoryItem{}, fmt.Errorf("memory is not configured")
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return MemoryItem{}, fmt.Errorf("memory item operations are not supported")
	}
	return service.GetMemoryItem(ctx, userID, itemID)
}

func (r *Runtime) UpdateMemoryItem(ctx context.Context, userID string, item MemoryItem) (MemoryItem, error) {
	if r.memory == nil {
		return MemoryItem{}, fmt.Errorf("memory is not configured")
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return MemoryItem{}, fmt.Errorf("memory item operations are not supported")
	}
	item.UpdatedAt = time.Now().UTC()
	return service.UpdateMemoryItem(ctx, userID, item)
}

func (r *Runtime) ApplyMemoryFeedback(ctx context.Context, userID, itemID, feedbackType string) (MemoryItem, error) {
	item, err := r.GetMemoryItem(ctx, userID, itemID)
	if err != nil {
		return MemoryItem{}, err
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	switch strings.ToLower(strings.TrimSpace(feedbackType)) {
	case "important":
		item.Weight = clamp01(item.Weight + 0.15)
		item.Metadata["feedback"] = "important"
	case "incorrect":
		item.Weight = clamp01(item.Weight - 0.25)
		item.Status = MemoryStatusArchived
		item.Metadata["feedback"] = "incorrect"
	case "not_relevant":
		item.Weight = clamp01(item.Weight - 0.10)
		item.Metadata["feedback"] = "not_relevant"
	default:
		return MemoryItem{}, fmt.Errorf("unsupported memory feedback type")
	}
	item.UpdatedAt = time.Now().UTC()
	return r.UpdateMemoryItem(ctx, userID, item)
}

func (r *Runtime) ResolveMemoryConflict(ctx context.Context, userID, itemID, action string) (MemoryItem, error) {
	item, err := r.GetMemoryItem(ctx, userID, itemID)
	if err != nil {
		return MemoryItem{}, err
	}
	if item.Status != MemoryStatusPendingConfirm && item.Status != MemoryStatusConflicted {
		return MemoryItem{}, fmt.Errorf("memory item is not pending confirmation")
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	now := time.Now().UTC()
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "accept":
		item.Status = MemoryStatusActive
		item.Metadata["conflict_resolution"] = "accepted"
		for _, conflictID := range item.ConflictIDs {
			conflict, err := r.GetMemoryItem(ctx, userID, conflictID)
			if err != nil {
				continue
			}
			conflict.Status = MemoryStatusArchived
			conflict.SupersededByID = item.ID
			conflict.UpdatedAt = now
			if conflict.Metadata == nil {
				conflict.Metadata = map[string]any{}
			}
			conflict.Metadata["conflict_resolution"] = "superseded_by_user_confirmed_memory"
			if _, err := r.UpdateMemoryItem(ctx, userID, conflict); err != nil {
				return MemoryItem{}, err
			}
		}
	case "reject":
		item.Status = MemoryStatusArchived
		item.Metadata["conflict_resolution"] = "rejected"
	case "keep_both":
		item.Status = MemoryStatusActive
		item.Metadata["conflict_resolution"] = "kept_both"
	default:
		return MemoryItem{}, fmt.Errorf("unsupported memory conflict action")
	}
	item.UpdatedAt = now
	return r.UpdateMemoryItem(ctx, userID, item)
}

func (r *Runtime) RebuildMemoryAbstractions(ctx context.Context, userID string) ([]MemoryItem, error) {
	if r.memory == nil || r.memoryAbstract == nil {
		return []MemoryItem{}, nil
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return nil, fmt.Errorf("memory item operations are not supported")
	}
	existing, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	abstracts, err := r.memoryAbstract.Build(ctx, userID, existing, now)
	if err != nil {
		return nil, err
	}
	builtHashes := map[string]bool{}
	updated := make([]MemoryItem, 0, len(abstracts))
	for _, item := range abstracts {
		item.UserID = userID
		item.SessionID = ""
		item.Source = MemorySourceSystem
		item.Status = MemoryStatusActive
		item = upsertMemoryItem(existing, item)
		item.Metadata["dirty"] = false
		saved, err := service.UpdateMemoryItem(ctx, userID, item)
		if err != nil {
			return nil, err
		}
		builtHashes[saved.RawHash] = true
		updated = append(updated, saved)
		existing = append(existing, saved)
	}
	for _, item := range existing {
		item = normalizeMemoryItem(item)
		if item.Source != MemorySourceSystem || (item.Level != MemoryLevelConcept && item.Level != MemoryLevelProfile) || builtHashes[item.RawHash] {
			continue
		}
		item.Status = MemoryStatusArchived
		item.UpdatedAt = now
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["archived_reason"] = "abstraction_rebuild_no_source_atoms"
		if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

func (r *Runtime) ScoreMemoryQuality(ctx context.Context, userID string) ([]MemoryItem, error) {
	service, err := r.memoryItemService()
	if err != nil {
		return nil, err
	}
	items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	updated := make([]MemoryItem, 0, len(items))
	for _, item := range items {
		scored := scoreMemoryQuality(item, items, now)
		if _, err := service.UpdateMemoryItem(ctx, userID, scored); err != nil {
			return nil, err
		}
		updated = append(updated, scored)
	}
	return updated, nil
}

func (r *Runtime) PlanMemoryMaintenance(ctx context.Context, userID string) ([]MemoryMaintenanceAction, error) {
	if r.memoryOrganizer == nil {
		return []MemoryMaintenanceAction{}, nil
	}
	service, err := r.memoryItemService()
	if err != nil {
		return nil, err
	}
	items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	scored := make([]MemoryItem, 0, len(items))
	for _, item := range items {
		scored = append(scored, scoreMemoryQuality(item, items, now))
	}
	return r.memoryOrganizer.Plan(ctx, userID, scored, now)
}

func (r *Runtime) ApplyMemoryMaintenance(ctx context.Context, userID, actionID string) (MemoryMaintenanceAction, error) {
	actions, err := r.PlanMemoryMaintenance(ctx, userID)
	if err != nil {
		return MemoryMaintenanceAction{}, err
	}
	var action MemoryMaintenanceAction
	for _, candidate := range actions {
		if memoryMaintenanceActionMatches(candidate, actionID) {
			action = candidate
			break
		}
	}
	if action.ID == "" {
		return MemoryMaintenanceAction{}, fmt.Errorf("memory maintenance action not found")
	}
	service, err := r.memoryItemService()
	if err != nil {
		return MemoryMaintenanceAction{}, err
	}
	switch action.Type {
	case "archive_low_quality":
		for _, memoryID := range action.MemoryIDs {
			item, err := service.GetMemoryItem(ctx, userID, memoryID)
			if err != nil {
				continue
			}
			item.Status = MemoryStatusArchived
			if item.Metadata == nil {
				item.Metadata = map[string]any{}
			}
			item.Metadata["maintenance_action"] = action.Type
			if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
				return MemoryMaintenanceAction{}, err
			}
		}
	case "merge_duplicates":
		if len(action.MemoryIDs) < 2 {
			break
		}
		winner, err := service.GetMemoryItem(ctx, userID, action.MemoryIDs[0])
		if err != nil {
			return MemoryMaintenanceAction{}, err
		}
		if winner.Metadata == nil {
			winner.Metadata = map[string]any{}
		}
		for _, memoryID := range action.MemoryIDs[1:] {
			loser, err := service.GetMemoryItem(ctx, userID, memoryID)
			if err != nil {
				continue
			}
			loser.Status = MemoryStatusArchived
			loser.SupersededByID = winner.ID
			if loser.Metadata == nil {
				loser.Metadata = map[string]any{}
			}
			loser.Metadata["maintenance_action"] = action.Type
			winner.RelatedIDs = append(winner.RelatedIDs, loser.ID)
			if _, err := service.UpdateMemoryItem(ctx, userID, loser); err != nil {
				return MemoryMaintenanceAction{}, err
			}
		}
		winner.RelatedIDs = normalizeMemoryIDs(winner.RelatedIDs)
		winner.Metadata["maintenance_action"] = action.Type
		if _, err := service.UpdateMemoryItem(ctx, userID, winner); err != nil {
			return MemoryMaintenanceAction{}, err
		}
	case "rebuild_concept", "refresh_profile":
		if _, err := r.RebuildMemoryAbstractions(ctx, userID); err != nil {
			return MemoryMaintenanceAction{}, err
		}
	case "confirm_conflict":
		// User-facing action; applying only leaves the item pending and records that it was surfaced.
		for _, memoryID := range action.MemoryIDs {
			item, err := service.GetMemoryItem(ctx, userID, memoryID)
			if err != nil {
				continue
			}
			if item.Metadata == nil {
				item.Metadata = map[string]any{}
			}
			item.Metadata["maintenance_action"] = "confirm_conflict_surfaced"
			if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
				return MemoryMaintenanceAction{}, err
			}
		}
	case "reduce_weight":
		for _, memoryID := range action.MemoryIDs {
			item, err := service.GetMemoryItem(ctx, userID, memoryID)
			if err != nil {
				continue
			}
			item.Weight = clamp01(item.Weight - 0.10)
			if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
				return MemoryMaintenanceAction{}, err
			}
		}
	default:
		return MemoryMaintenanceAction{}, fmt.Errorf("unsupported memory maintenance action")
	}
	action.Status = MemoryMaintenanceApplied
	return action, nil
}

func (r *Runtime) DismissMemoryMaintenance(ctx context.Context, userID, actionID string) (MemoryMaintenanceAction, error) {
	actions, err := r.PlanMemoryMaintenance(ctx, userID)
	if err != nil {
		return MemoryMaintenanceAction{}, err
	}
	for _, action := range actions {
		if memoryMaintenanceActionMatches(action, actionID) {
			action.Status = MemoryMaintenanceDismissed
			return action, nil
		}
	}
	return MemoryMaintenanceAction{}, fmt.Errorf("memory maintenance action not found")
}

func (r *Runtime) memoryItemService() (MemoryItemService, error) {
	if r.memory == nil {
		return nil, fmt.Errorf("memory is not configured")
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return nil, fmt.Errorf("memory item operations are not supported")
	}
	return service, nil
}

func (r *Runtime) DeleteMemoryItem(ctx context.Context, userID, itemID string) error {
	if r.memory == nil {
		return nil
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return nil
	}
	return service.DeleteMemoryItem(ctx, userID, itemID)
}

func (r *Runtime) ExportUserData(ctx context.Context, user *UserProfile) (*UserDataExport, error) {
	if user == nil || strings.TrimSpace(user.ID) == "" {
		return nil, fmt.Errorf("user is required")
	}
	sessions, err := r.ListSessions(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out := &UserDataExport{
		ExportedAt: time.Now().UTC(),
		User:       user,
		Sessions:   sessions,
		Memory: MemoryExport{
			Sessions: make(map[string]string),
		},
	}
	if messages, ok := r.sessions.(interface {
		ListMessages(context.Context, string, string) ([]state.Message, error)
	}); ok {
		out.Messages = make(map[string][]state.Message, len(sessions))
		for _, session := range sessions {
			items, err := messages.ListMessages(ctx, user.ID, session.ID)
			if err != nil {
				return nil, err
			}
			if len(items) == 0 {
				items = session.Messages
			}
			out.Messages[session.ID] = items
		}
	}
	if r.artifacts != nil {
		attachments, err := r.artifacts.List(ctx, user.ID, "", AssetKindAttachment)
		if err != nil {
			return nil, err
		}
		artifacts, err := r.artifacts.List(ctx, user.ID, "", AssetKindArtifact)
		if err != nil {
			return nil, err
		}
		out.Attachments = attachments
		out.Artifacts = artifacts
	}
	if r.jobs != nil {
		jobs, err := r.jobs.ListJobs(ctx, user.ID, "")
		if err != nil {
			return nil, err
		}
		out.Jobs = jobs
		out.JobEvents = make(map[string][]*JobEvent, len(jobs))
		for _, job := range jobs {
			events, err := r.jobs.ListJobEvents(ctx, user.ID, job.ID, "", 0)
			if err != nil {
				return nil, err
			}
			out.JobEvents[job.ID] = events
		}
	}
	if r.memory == nil {
		return out, nil
	}
	if service, ok := r.memory.(MemoryItemService); ok {
		items, err := service.ListMemoryItems(ctx, user.ID, MemoryItemFilter{Status: ""})
		if err != nil {
			return nil, err
		}
		out.Memory.Items = items
	}
	userMemory, err := r.memory.LoadUserMemory(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out.Memory.User = userMemory
	for _, session := range sessions {
		content, err := r.memory.LoadSessionMemory(ctx, user.ID, session.ID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(content) != "" {
			out.Memory.Sessions[session.ID] = content
		}
	}
	return out, nil
}

func (r *Runtime) DeleteUserData(ctx context.Context, userID string) error {
	for _, session := range r.runningSessionIDs(userID) {
		r.Cancel(userID, session)
	}
	if r.memory != nil {
		if err := r.memory.DeleteUser(ctx, userID); err != nil {
			return err
		}
	}
	if r.sessions != nil {
		if err := r.sessions.DeleteUser(ctx, userID); err != nil {
			return err
		}
	}
	if r.artifacts != nil {
		if err := r.artifacts.DeleteUser(ctx, userID); err != nil {
			return err
		}
	}
	if r.jobs != nil {
		if err := r.jobs.DeleteUser(ctx, userID); err != nil {
			return err
		}
	}
	if r.config.UserWorkspaceRoot != "" {
		if err := os.RemoveAll(r.userWorkspace(userID)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) PruneBefore(ctx context.Context, cutoff time.Time) (map[string]int, error) {
	out := make(map[string]int)
	if r.memory != nil {
		n, err := r.memory.PruneBefore(ctx, cutoff)
		if err != nil {
			return out, err
		}
		out["memories"] = n
	}
	if r.sessions != nil {
		n, err := r.sessions.PruneBefore(ctx, cutoff)
		if err != nil {
			return out, err
		}
		out["sessions"] = n
	}
	if r.artifacts != nil {
		n, err := r.artifacts.PruneDeletedBefore(ctx, cutoff)
		if err != nil {
			return out, err
		}
		out["artifacts"] = n
	}
	if r.jobs != nil {
		n, err := r.jobs.PruneBefore(ctx, cutoff)
		if err != nil {
			return out, err
		}
		out["jobs"] = n
	}
	return out, nil
}

func (r *Runtime) CreateArtifact(ctx context.Context, userID, sessionID, filename, contentType string, data []byte) (*Artifact, error) {
	return r.createAsset(ctx, AssetKindArtifact, userID, sessionID, filename, contentType, data)
}

func (r *Runtime) CreateAttachment(ctx context.Context, userID, sessionID, filename, contentType string, data []byte) (*Artifact, error) {
	return r.createAsset(ctx, AssetKindAttachment, userID, sessionID, filename, contentType, data)
}

func (r *Runtime) createAsset(ctx context.Context, kind, userID, sessionID, filename, contentType string, data []byte) (*Artifact, error) {
	if r.artifacts == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	if strings.TrimSpace(sessionID) != "" {
		if _, err := r.GetSession(ctx, userID, sessionID); err != nil {
			return nil, err
		}
	}
	asset, err := r.artifacts.Create(ctx, kind, userID, sessionID, filename, contentType, data)
	if err != nil {
		return nil, err
	}
	r.scanCreatedAsset(ctx, asset, data)
	return asset, nil
}

func (r *Runtime) scanCreatedAsset(ctx context.Context, asset *Artifact, data []byte) {
	if r == nil || r.riskScanner == nil || r.riskRecorder == nil || asset == nil {
		return
	}
	findings := r.riskScanner.ScanRisk(ctx, RiskScanTarget{
		Kind:        asset.Kind,
		UserID:      asset.UserID,
		SessionID:   asset.SessionID,
		JobID:       asset.JobID,
		AssetID:     asset.ID,
		Filename:    asset.Filename,
		ContentType: asset.ContentType,
		Data:        data,
	})
	for _, finding := range findings {
		metadata := map[string]any{
			"category":     finding.Category,
			"snippet":      finding.Snippet,
			"target":       asset.Kind,
			"filename":     asset.Filename,
			"content_type": asset.ContentType,
			"size_bytes":   asset.SizeBytes,
		}
		for key, value := range finding.Metadata {
			metadata[key] = value
		}
		r.riskRecorder(ctx, RiskEvent{
			UserID:     asset.UserID,
			SessionID:  asset.SessionID,
			JobID:      asset.JobID,
			AssetID:    asset.ID,
			Operation:  riskOperationForScanTarget(asset.Kind),
			Reason:     finding.Reason,
			RiskLevel:  finding.RiskLevel,
			ScoreDelta: finding.ScoreDelta,
			Metadata:   metadata,
		})
	}
}

func (r *Runtime) recordExecutionDenialRisk(ctx context.Context, userID, sessionID, skillName string, err error, metadata map[string]any) {
	if r == nil || r.riskRecorder == nil {
		return
	}
	finding, ok := executionDenialFinding(err)
	if !ok {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["category"] = finding.Category
	metadata["snippet"] = finding.Snippet
	if strings.TrimSpace(skillName) != "" {
		metadata["skill_name"] = skillName
	}
	r.riskRecorder(ctx, RiskEvent{
		UserID:     userID,
		SessionID:  sessionID,
		JobID:      jobIDFromContext(ctx),
		RequestID:  requestIDFromContext(ctx),
		Operation:  "execution_denied",
		Reason:     finding.Reason,
		RiskLevel:  finding.RiskLevel,
		ScoreDelta: finding.ScoreDelta,
		Metadata:   metadata,
	})
}

func (r *Runtime) ListArtifacts(ctx context.Context, userID, sessionID string) ([]*Artifact, error) {
	return r.listAssets(ctx, AssetKindArtifact, userID, sessionID)
}

func (r *Runtime) ListAttachments(ctx context.Context, userID, sessionID string) ([]*Artifact, error) {
	return r.listAssets(ctx, AssetKindAttachment, userID, sessionID)
}

func (r *Runtime) listAssets(ctx context.Context, kind, userID, sessionID string) ([]*Artifact, error) {
	if r.artifacts == nil {
		return []*Artifact{}, nil
	}
	return r.artifacts.List(ctx, userID, sessionID, kind)
}

func (r *Runtime) GetArtifact(ctx context.Context, userID, artifactID string) (*Artifact, []byte, error) {
	return r.getAsset(ctx, AssetKindArtifact, userID, artifactID)
}

func (r *Runtime) GetAttachment(ctx context.Context, userID, attachmentID string) (*Artifact, []byte, error) {
	return r.getAsset(ctx, AssetKindAttachment, userID, attachmentID)
}

func (r *Runtime) GetAttachmentMetadata(ctx context.Context, userID, attachmentID string) (*Artifact, error) {
	if r.artifacts == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	return r.artifacts.GetMetadata(ctx, userID, attachmentID, AssetKindAttachment)
}

func (r *Runtime) getAsset(ctx context.Context, kind, userID, assetID string) (*Artifact, []byte, error) {
	if r.artifacts == nil {
		return nil, nil, fmt.Errorf("artifact service is not configured")
	}
	return r.artifacts.Get(ctx, userID, assetID, kind)
}

func (r *Runtime) DeleteArtifact(ctx context.Context, userID, artifactID string) error {
	return r.deleteAsset(ctx, AssetKindArtifact, userID, artifactID)
}

func (r *Runtime) DeleteAttachment(ctx context.Context, userID, attachmentID string) error {
	return r.deleteAsset(ctx, AssetKindAttachment, userID, attachmentID)
}

func (r *Runtime) deleteAsset(ctx context.Context, kind, userID, assetID string) error {
	if r.artifacts == nil {
		return nil
	}
	return r.artifacts.Delete(ctx, userID, assetID, kind)
}

func (r *Runtime) Chat(ctx context.Context, req ChatRequest, sink EventSink) error {
	if strings.TrimSpace(req.UserID) == "" {
		return fmt.Errorf("user ID is required")
	}
	if strings.TrimSpace(req.Content) == "" && len(req.AttachmentIDs) == 0 && len(req.AttachmentURLs) == 0 {
		return fmt.Errorf("content or attachment is required")
	}
	if sink == nil {
		return fmt.Errorf("event sink is required")
	}
	session, err := r.GetSession(ctx, req.UserID, req.SessionID)
	if err != nil {
		return err
	}
	if err := r.injectMemory(ctx, req.UserID, session); err != nil {
		return err
	}

	turnCtx, cancel := context.WithTimeout(ctx, r.config.TurnTimeout)
	if err := r.start(sessionKey(req.UserID, session.ID), cancel); err != nil {
		cancel()
		return err
	}
	defer r.finish(sessionKey(req.UserID, session.ID))

	if err := sink.Send(ctx, Event{Type: "start", SessionID: session.ID}); err != nil {
		return err
	}
	displayContent := req.Content
	if strings.TrimSpace(displayContent) == "" {
		displayContent = "Please analyze the attached file(s)."
	}
	if err := sink.Send(ctx, Event{Type: "message", SessionID: session.ID, Role: "user", Content: displayContent}); err != nil {
		return err
	}

	result, err := r.run(turnCtx, req, session, func(token string) {
		_ = sink.Send(ctx, Event{Type: "delta", SessionID: session.ID, Role: "assistant", Content: token})
	})
	if err != nil {
		r.appendFailedTurn(session, displayContent, err)
		if saveErr := r.sessions.Save(ctx, req.UserID, session); saveErr != nil {
			_ = sink.Send(ctx, Event{Type: "error", SessionID: session.ID, Error: err.Error()})
			return errors.Join(err, saveErr)
		}
		_ = sink.Send(ctx, Event{Type: "error", SessionID: session.ID, Error: err.Error()})
		return err
	}
	session = result.Session
	if session == nil {
		return fmt.Errorf("runner returned no session")
	}
	if err := r.sessions.Save(ctx, req.UserID, session); err != nil {
		return err
	}
	if r.memory != nil {
		if err := r.afterTurnMemory(ctx, req.UserID, session); err != nil {
			return err
		}
	}
	if err := sink.Send(ctx, Event{Type: "message", SessionID: session.ID, Role: "assistant", Content: result.Output}); err != nil {
		return err
	}
	return sink.Send(ctx, Event{Type: "done", SessionID: session.ID})
}

func (r *Runtime) afterTurnMemory(ctx context.Context, userID string, session *state.Session) error {
	if r.memory == nil || session == nil {
		return nil
	}
	settings, err := r.GetMemorySettings(ctx, userID)
	if err != nil {
		return err
	}
	if !settings.CaptureEnabled {
		return nil
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok || r.memoryExtract == nil {
		return r.memory.AfterTurn(ctx, userID, session)
	}
	candidates, err := r.memoryExtract.Extract(ctx, MemoryExtractionInput{
		UserID:    userID,
		SessionID: session.ID,
		Messages:  session.Messages,
		Now:       time.Now().UTC(),
	})
	if err != nil {
		return r.memory.AfterTurn(ctx, userID, session)
	}
	items := evaluateMemoryCandidates(userID, session.ID, candidates)
	if len(items) == 0 {
		return nil
	}
	existing, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return err
	}
	for _, candidate := range items {
		var conflictUpdates []MemoryItem
		candidate, conflictUpdates = applyMemoryConflictResolution(existing, candidate)
		for _, update := range conflictUpdates {
			if _, err := service.UpdateMemoryItem(ctx, userID, update); err != nil {
				return err
			}
			existing = append(existing, update)
		}
		item := upsertMemoryItem(existing, candidate)
		if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
			return err
		}
		existing = append(existing, item)
	}
	if err := r.markMemoryAbstractionsDirty(ctx, userID, service); err != nil {
		return err
	}
	return nil
}

func (r *Runtime) markMemoryAbstractionsDirty(ctx context.Context, userID string, service MemoryItemService) error {
	items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{Status: MemoryStatusActive})
	if err != nil {
		return err
	}
	for _, item := range items {
		item = normalizeMemoryItem(item)
		if item.Source != MemorySourceSystem || (item.Level != MemoryLevelConcept && item.Level != MemoryLevelProfile) {
			continue
		}
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["dirty"] = true
		item.UpdatedAt = time.Now().UTC()
		if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) appendFailedTurn(session *state.Session, userContent string, runErr error) {
	if session == nil || runErr == nil {
		return
	}
	ensureVisibleUserMessage(session, userContent)
	session.AddAssistantMessage("Request failed: " + runErr.Error())
}

func ensureVisibleUserMessage(session *state.Session, content string) {
	if session == nil || strings.TrimSpace(content) == "" {
		return
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Hidden {
			continue
		}
		if msg.Role == "user" && msg.Content == content {
			return
		}
		if msg.Role == "user" || msg.Role == "assistant" {
			break
		}
	}
	session.AddUserMessage(content)
}

func (r *Runtime) CreateJob(ctx context.Context, req ChatRequest, jobType string) (*Job, error) {
	if r.jobs == nil {
		return nil, fmt.Errorf("job store is not configured")
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	if strings.TrimSpace(req.Content) == "" && len(req.AttachmentIDs) == 0 && len(req.AttachmentURLs) == 0 {
		return nil, fmt.Errorf("content or attachment is required")
	}
	if _, err := r.GetSession(ctx, req.UserID, req.SessionID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	job := &Job{
		ID:             NewJobID(),
		UserID:         req.UserID,
		SessionID:      req.SessionID,
		Type:           firstNonEmptyString(jobType, "chat"),
		Status:         JobStatusQueued,
		Content:        req.Content,
		AttachmentIDs:  append([]string(nil), req.AttachmentIDs...),
		AttachmentURLs: append([]ChatAttachmentURL(nil), req.AttachmentURLs...),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := r.jobs.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (r *Runtime) StartJob(ctx context.Context, job *Job) error {
	if r.jobs == nil {
		return fmt.Errorf("job store is not configured")
	}
	if job == nil {
		return fmt.Errorf("job is required")
	}
	if _, err := r.jobs.GetJob(ctx, job.UserID, job.ID); err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	workerCtx = withRequestID(workerCtx, requestIDFromContext(ctx))
	workerCtx = WithJobID(workerCtx, job.ID)
	if err := r.startJob(job.ID, cancel); err != nil {
		cancel()
		return err
	}
	go r.runJob(workerCtx, job)
	return nil
}

func (r *Runtime) runJob(ctx context.Context, job *Job) {
	defer r.finishJob(job.ID)
	now := time.Now().UTC()
	if err := r.jobs.UpdateJobStatus(ctx, job.UserID, job.ID, JobStatusRunning, "", now); err != nil {
		return
	}
	sink := &jobEventSink{store: r.jobs, broker: r.jobEvents, job: job}
	err := r.Chat(ctx, ChatRequest{UserID: job.UserID, SessionID: job.SessionID, Content: job.Content, AttachmentIDs: job.AttachmentIDs, AttachmentURLs: job.AttachmentURLs}, sink)
	finishedAt := time.Now().UTC()
	switch {
	case err == nil:
		_ = r.jobs.UpdateJobStatus(context.Background(), job.UserID, job.ID, JobStatusSucceeded, "", finishedAt)
	case errors.Is(err, context.Canceled) || errors.Is(err, ErrRuntimeShuttingDown):
		_ = r.jobs.UpdateJobStatus(context.Background(), job.UserID, job.ID, JobStatusCancelled, err.Error(), finishedAt)
		_ = sink.Send(context.Background(), Event{Type: "cancelled", SessionID: job.SessionID, JobID: job.ID})
	default:
		_ = r.jobs.UpdateJobStatus(context.Background(), job.UserID, job.ID, JobStatusFailed, err.Error(), finishedAt)
		if !strings.HasPrefix(strings.TrimSpace(job.Content), "/") {
			r.recordExecutionDenialRisk(ctx, job.UserID, job.SessionID, "", err, map[string]any{"phase": "job", "job_type": job.Type})
		}
	}
}

func (r *Runtime) GetJob(ctx context.Context, userID, jobID string) (*Job, error) {
	if r.jobs == nil {
		return nil, fmt.Errorf("job store is not configured")
	}
	return r.jobs.GetJob(ctx, userID, jobID)
}

func (r *Runtime) ListJobs(ctx context.Context, userID, sessionID string) ([]*Job, error) {
	if r.jobs == nil {
		return []*Job{}, nil
	}
	return r.jobs.ListJobs(ctx, userID, sessionID)
}

func (r *Runtime) SearchMessages(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	store, ok := r.sessions.(MessageSearchStore)
	if !ok || store == nil {
		return []MessageSearchResult{}, nil
	}
	return store.SearchMessages(ctx, userID, query, limit, offset)
}

func (r *Runtime) ListSkillExecutions(ctx context.Context, filter SkillExecutionFilter) ([]SkillExecutionRecord, error) {
	if r == nil || r.skillExecutions == nil {
		return []SkillExecutionRecord{}, nil
	}
	return r.skillExecutions.ListSkillExecutions(ctx, filter)
}

func (r *Runtime) SummarizeSkillExecutions(ctx context.Context, filter SkillExecutionFilter) (SkillExecutionSummary, error) {
	if r == nil || r.skillExecutions == nil {
		return SkillExecutionSummary{SkillName: strings.TrimSpace(filter.SkillName)}, nil
	}
	return r.skillExecutions.SummarizeSkillExecutions(ctx, filter)
}

func (r *Runtime) ListJobEvents(ctx context.Context, userID, jobID, afterID string, limit int) ([]*JobEvent, error) {
	if r.jobs == nil {
		return []*JobEvent{}, nil
	}
	return r.jobs.ListJobEvents(ctx, userID, jobID, afterID, limit)
}

func (r *Runtime) CancelJob(ctx context.Context, userID, jobID string) error {
	if r.jobs == nil {
		return fmt.Errorf("job store is not configured")
	}
	job, err := r.jobs.GetJob(ctx, userID, jobID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	cancel, ok := r.runningJobs[jobID]
	r.mu.Unlock()
	if ok {
		cancel()
		return nil
	}
	if isTerminalJobStatus(job.Status) {
		return nil
	}
	now := time.Now().UTC()
	if err := r.jobs.UpdateJobStatus(ctx, userID, jobID, JobStatusCancelled, "cancelled before execution", now); err != nil {
		return err
	}
	return (&jobEventSink{store: r.jobs, broker: r.jobEvents, job: job}).Send(ctx, Event{Type: "cancelled", SessionID: job.SessionID, JobID: job.ID})
}

func (r *Runtime) Cancel(userID, sessionID string) bool {
	key := sessionKey(userID, sessionID)
	r.mu.Lock()
	cancel, ok := r.running[key]
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.shuttingDown = true
	cancels := make([]context.CancelFunc, 0, len(r.running)+len(r.runningJobs))
	for _, cancel := range r.running {
		cancels = append(cancels, cancel)
	}
	for _, cancel := range r.runningJobs {
		cancels = append(cancels, cancel)
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) ListSkills() []*skills.SkillDefinition {
	if r.skills == nil {
		return nil
	}
	return r.skills.ListUserInvocableSkills()
}

func (r *Runtime) RouteChat(req ChatRequest) JobRoutingDecision {
	content := strings.TrimSpace(req.Content)
	if content == "" || r.jobs == nil {
		return JobRoutingDecision{}
	}
	if skill, ok := r.skillForPrompt(content); ok {
		if skill.RunAsJob || skill.ExecutionContext == skills.ContextFork {
			return JobRoutingDecision{RunAsJob: true, JobType: "skill", Reason: "skill metadata requests durable job execution"}
		}
		return JobRoutingDecision{}
	}
	if contentLikelyLongRunning(content) {
		return JobRoutingDecision{RunAsJob: true, JobType: "chat", Reason: "request is likely to create artifacts or run for a long time"}
	}
	return JobRoutingDecision{}
}

func (r *Runtime) skillForPrompt(content string) (*skills.SkillDefinition, bool) {
	content = strings.TrimSpace(content)
	if content == "" || r.skills == nil {
		return nil, false
	}
	if strings.HasPrefix(content, "/") {
		parts := strings.SplitN(content, " ", 2)
		name := strings.TrimPrefix(parts[0], "/")
		if name == "" || name == "skills" {
			return nil, false
		}
		skill, ok := r.skills.GetSkill(name)
		if !ok || !skill.UserInvocable {
			return nil, false
		}
		return skill, true
	}
	return r.skills.MatchUserInvocableSkill(content)
}

func contentLikelyLongRunning(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	phrases := []string{
		"生成ppt", "制作ppt", "做ppt", "生成幻灯片", "制作幻灯片",
		"生成图片", "生成一张图", "画一张", "生图", "生成视频", "生成音频",
		"生成文件", "导出文件", "导出", "批量处理", "批量生成",
		"爬取", "抓取", "生成报告", "长任务", "执行工作流",
		"分析项目", "分析代码库", "分析整个项目", "分析仓库",
		"generate ppt", "create ppt", "presentation", "slide deck", "slides",
		"generate image", "create image", "render image", "generate video", "generate audio",
		"generate file", "export file", "batch process", "batch generate",
		"crawl", "scrape", "generate report", "long-running", "run workflow",
		"analyze codebase", "analyze repository", "analyze project",
	}
	for _, phrase := range phrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func (r *Runtime) injectMemory(ctx context.Context, userID string, session *state.Session) error {
	if r.memory == nil || session == nil {
		return nil
	}
	settings, err := r.GetMemorySettings(ctx, userID)
	if err != nil {
		return err
	}
	if !settings.ContextEnabled {
		return nil
	}
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	if session.Metadata[memoryInjectedKey] == "true" {
		return nil
	}
	content, err := r.memory.LoadContext(ctx, userID, session)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	session.AddSystemContext("<memory>\n" + content + "\n</memory>")
	session.Metadata[memoryInjectedKey] = "true"
	return nil
}

func (r *Runtime) run(ctx context.Context, req ChatRequest, session *state.Session, onToken func(string)) (runnerResult, error) {
	userID := req.UserID
	content := req.Content
	if strings.TrimSpace(content) == "" {
		content = "Please analyze the attached file(s)."
	}
	ctx = WithLLMScope(ctx, LLMScope{
		UserID:    userID,
		SessionID: session.ID,
		JobID:     jobIDFromContext(ctx),
		RequestID: requestIDFromContext(ctx),
	})
	ensureConsumerSecurityContext(session)
	if strings.HasPrefix(strings.TrimSpace(content), "/") {
		return r.runSkillCommand(ctx, req, userID, session, content, onToken)
	}
	prompt, err := r.chatPrompt(ctx, req, content)
	if err != nil {
		return runnerResult{}, err
	}
	runner := r.runnerForScope(Scope{
		UserID:     userID,
		SessionID:  session.ID,
		WorkingDir: session.WorkingDir,
	})
	result, err := runWithTokenStreamContent(ctx, runner, session, prompt, false, onToken)
	return runnerResult{Output: result.Output, Session: result.Session}, err
}

func ensureConsumerSecurityContext(session *state.Session) {
	if session == nil {
		return
	}
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	if session.Metadata[consumerSecurityInjectedKey] == "true" {
		return
	}
	session.AddSystemContext(consumerSecuritySystemContext)
	session.Metadata[consumerSecurityInjectedKey] = "true"
}

const vertexInlineAttachmentLimitBytes = 20 << 20
const textAttachmentPromptLimitBytes = 1 << 20
const signedAttachmentURLTTL = 15 * time.Minute

func (r *Runtime) chatPrompt(ctx context.Context, req ChatRequest, content string) ([]publictypes.ContentBlock, error) {
	blocks := []publictypes.ContentBlock{{Type: "text", Text: content}}
	attachmentNames := make([]string, 0, len(req.AttachmentIDs)+len(req.AttachmentURLs))
	for _, id := range req.AttachmentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		artifact, err := r.GetAttachmentMetadata(ctx, req.UserID, id)
		if err != nil {
			return nil, fmt.Errorf("load attachment %s: %w", id, err)
		}
		if artifact.SessionID != "" && artifact.SessionID != req.SessionID {
			return nil, fmt.Errorf("attachment %s does not belong to this session", id)
		}
		attachmentNames = append(attachmentNames, artifact.Filename)
		if isTextAttachment(artifact.Filename, artifact.ContentType) {
			_, data, err := r.GetAttachment(ctx, req.UserID, id)
			if err != nil {
				return nil, fmt.Errorf("load attachment %s: %w", id, err)
			}
			if len(data) > textAttachmentPromptLimitBytes {
				return nil, fmt.Errorf("text attachment %s exceeds prompt inline limit of %d bytes", artifact.Filename, textAttachmentPromptLimitBytes)
			}
			blocks = append(blocks, publictypes.ContentBlock{
				Type: "text",
				Text: formatTextAttachmentPrompt(artifact.Filename, artifact.ContentType, data),
			})
			continue
		}
		if block, ok, err := r.presignedAttachmentBlock(ctx, artifact); ok && err == nil {
			blocks = append(blocks, block)
			continue
		} else if ok && err != nil && artifact.SizeBytes > vertexInlineAttachmentLimitBytes {
			return nil, fmt.Errorf("presign attachment %s: %w", artifact.Filename, err)
		}
		_, data, err := r.GetAttachment(ctx, req.UserID, id)
		if err != nil {
			return nil, fmt.Errorf("load attachment %s: %w", id, err)
		}
		if int64(len(data)) > vertexInlineAttachmentLimitBytes {
			return nil, fmt.Errorf("attachment %s exceeds Vertex inlineData limit of %d bytes", artifact.Filename, vertexInlineAttachmentLimitBytes)
		}
		blocks = append(blocks, publictypes.ContentBlock{
			Type: attachmentBlockType(artifact.ContentType),
			Source: map[string]interface{}{
				"type":       "base64",
				"media_type": artifact.ContentType,
				"data":       base64.StdEncoding.EncodeToString(data),
			},
		})
	}
	for _, item := range req.AttachmentURLs {
		fileURL := strings.TrimSpace(item.URL)
		if fileURL == "" {
			continue
		}
		parsedURL, err := url.Parse(fileURL)
		if err != nil {
			return nil, fmt.Errorf("invalid attachment URL %q: %w", fileURL, err)
		}
		if parsedURL.Scheme == "" {
			return nil, fmt.Errorf("invalid attachment URL %q: scheme is required", fileURL)
		}
		contentType := strings.TrimSpace(item.ContentType)
		if contentType == "" {
			contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(parsedURL.Path)))
		}
		if contentType == "" {
			return nil, fmt.Errorf("content_type is required for attachment URL %q", fileURL)
		}
		attachmentNames = append(attachmentNames, firstNonEmptyString(item.Filename, filepath.Base(parsedURL.Path), fileURL))
		blocks = append(blocks, publictypes.ContentBlock{
			Type: attachmentBlockType(contentType),
			Source: map[string]interface{}{
				"type":       "url",
				"media_type": normalizedContentType(contentType),
				"file_uri":   fileURL,
			},
		})
	}
	if len(attachmentNames) > 0 {
		blocks[0].Text = content + "\n\nAttached files: " + strings.Join(attachmentNames, ", ")
	}
	return blocks, nil
}

func (r *Runtime) presignedAttachmentBlock(ctx context.Context, artifact *Artifact) (publictypes.ContentBlock, bool, error) {
	if r == nil || r.artifacts == nil || artifact == nil {
		return publictypes.ContentBlock{}, false, nil
	}
	fileURL, ok, err := r.artifacts.PresignGet(ctx, artifact.ObjectKey, signedAttachmentURLTTL)
	if !ok || err != nil {
		return publictypes.ContentBlock{}, ok, err
	}
	return publictypes.ContentBlock{
		Type: attachmentBlockType(artifact.ContentType),
		Source: map[string]interface{}{
			"type":       "url",
			"media_type": normalizedContentType(artifact.ContentType),
			"file_uri":   fileURL,
		},
	}, true, nil
}

func formatTextAttachmentPrompt(filename, contentType string, data []byte) string {
	if strings.TrimSpace(contentType) == "" {
		contentType = "text/plain"
	}
	text := strings.ToValidUTF8(string(data), "\uFFFD")
	return fmt.Sprintf("\n\nAttached text file: %s\nContent-Type: %s\n\n```text\n%s\n```", filename, contentType, text)
}

func isTextAttachment(filename, contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(contentType, "text/") {
		return true
	}
	switch contentType {
	case "application/json", "application/ld+json", "application/xml", "application/yaml", "application/x-yaml", "application/toml":
		return true
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	switch ext {
	case "txt", "md", "markdown", "csv", "tsv", "json", "jsonl", "log", "yaml", "yml", "xml", "html", "htm", "css", "js", "jsx", "ts", "tsx", "go", "py", "java", "c", "cpp", "h", "sh", "sql", "toml", "ini", "env":
		return true
	default:
		return false
	}
}

func attachmentBlockType(contentType string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "image/") {
		return "image"
	}
	return "file"
}

func (r *Runtime) runSkillCommand(ctx context.Context, req ChatRequest, userID string, session *state.Session, content string, onToken func(string)) (runnerResult, error) {
	parts := strings.SplitN(strings.TrimSpace(content), " ", 2)
	name := strings.TrimPrefix(parts[0], "/")
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}
	if name == "skills" {
		return runnerResult{Output: formatSkillList(r.ListSkills()), Session: session}, nil
	}
	if r.skills == nil {
		return runnerResult{}, fmt.Errorf("skills are not configured")
	}
	skill, ok := r.skills.GetSkill(name)
	if !ok {
		return runnerResult{}, fmt.Errorf("unknown skill: /%s", name)
	}
	if !skill.UserInvocable {
		return runnerResult{}, fmt.Errorf("skill /%s is not user-invocable", name)
	}
	if len(req.AttachmentIDs) > 0 {
		attachmentContext, err := r.textAttachmentContext(ctx, req)
		if err != nil {
			return runnerResult{}, err
		}
		if attachmentContext != "" {
			args = strings.TrimSpace(args + "\n\n" + attachmentContext)
		}
	}
	session.AddUserMessage(content)
	return r.runSkill(ctx, userID, session, skill, args, onToken)
}

func (r *Runtime) textAttachmentContext(ctx context.Context, req ChatRequest) (string, error) {
	var parts []string
	for _, id := range req.AttachmentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		artifact, data, err := r.GetAttachment(ctx, req.UserID, id)
		if err != nil {
			return "", fmt.Errorf("load attachment %s: %w", id, err)
		}
		if artifact.SessionID != "" && artifact.SessionID != req.SessionID {
			return "", fmt.Errorf("attachment %s does not belong to this session", id)
		}
		if !isTextAttachment(artifact.Filename, artifact.ContentType) {
			continue
		}
		if len(data) > textAttachmentPromptLimitBytes {
			return "", fmt.Errorf("text attachment %s exceeds prompt inline limit of %d bytes", artifact.Filename, textAttachmentPromptLimitBytes)
		}
		parts = append(parts, formatTextAttachmentPrompt(artifact.Filename, artifact.ContentType, data))
	}
	return strings.Join(parts, "\n"), nil
}

func (r *Runtime) runSkill(ctx context.Context, userID string, session *state.Session, skill *skills.SkillDefinition, args string, onToken func(string)) (runnerResult, error) {
	startedAt := time.Now().UTC()
	status := SkillExecutionStatusFailed
	errText := ""
	var policy SkillRuntimePolicy
	defer func() {
		if r == nil || r.skillExecutions == nil || skill == nil {
			return
		}
		completedAt := time.Now().UTC()
		_ = r.skillExecutions.RecordSkillExecution(context.Background(), SkillExecutionRecord{
			SkillName:   skill.Name,
			UserID:      userID,
			SessionID:   session.ID,
			JobID:       jobIDFromContext(ctx),
			RequestID:   requestIDFromContext(ctx),
			Status:      status,
			Error:       errText,
			DurationMS:  completedAt.Sub(startedAt).Milliseconds(),
			StartedAt:   startedAt,
			CompletedAt: completedAt,
			Metadata: map[string]any{
				"args_length":       len(args),
				"allowed_tools":     policy.AllowedTools,
				"allowed_env":       policy.AllowedEnv,
				"network_allowlist": policy.NetworkAllowlist,
				"artifact_types":    policy.ArtifactTypes,
				"execution_context": string(skill.ExecutionContext),
				"run_as_job":        skill.RunAsJob,
			},
		})
	}()
	ctx = WithLLMScope(ctx, LLMScope{
		UserID:    userID,
		SessionID: session.ID,
		SkillName: skill.Name,
		JobID:     jobIDFromContext(ctx),
		RequestID: requestIDFromContext(ctx),
	})
	workspace := r.sandboxedWorkingDir(userID, session.WorkingDir)
	if workspace == "" {
		workspace = filepath.Clean(r.config.DefaultWorkingDir)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		errText = err.Error()
		return runnerResult{}, err
	}
	skillDir := workspace
	if strings.TrimSpace(skill.SkillRoot) != "" {
		skillDir = skill.SkillRoot
	}
	policy = r.skillRuntimePolicy(skill)
	shellTimeout := r.config.SkillShellTimeout
	if policy.ShellTimeout > 0 {
		shellTimeout = policy.ShellTimeout
	}
	blocks, err := skill.GetPrompt(args, &skills.SkillContext{
		SessionID:    session.ID,
		WorkingDir:   skillDir,
		Environment:  r.skillShellEnvironment(workspace, policy.AllowedEnv),
		ShellRuntime: r.skillShellRuntime(workspace, skillDir, skill, policy),
		ShellTimeout: shellTimeout,
	})
	if err != nil {
		errText = err.Error()
		r.recordExecutionDenialRisk(ctx, userID, session.ID, skill.Name, err, map[string]any{
			"phase":             "skill_prompt",
			"allowed_tools":     policy.AllowedTools,
			"allowed_env":       policy.AllowedEnv,
			"network_allowlist": policy.NetworkAllowlist,
			"sandbox_network":   policy.Sandbox.Network,
		})
		return runnerResult{}, err
	}
	var prompt strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			prompt.WriteString(block.Text)
		}
	}
	generated := skills.WrapGeneratedSkillPrompt(skill.Name, args, prompt.String())
	runner := r.runnerForScope(Scope{
		UserID:           userID,
		SessionID:        session.ID,
		WorkingDir:       workspace,
		SkillName:        skill.Name,
		SkillRoot:        skillDir,
		SkillScoped:      true,
		AllowedTools:     policy.AllowedTools,
		AllowedEnv:       policy.AllowedEnv,
		NetworkAllowlist: policy.NetworkAllowlist,
		ArtifactTypes:    policy.ArtifactTypes,
	})
	result, err := runWithTokenStream(ctx, runner, session, generated, true, onToken)
	if err != nil {
		errText = err.Error()
		r.recordExecutionDenialRisk(ctx, userID, session.ID, skill.Name, err, map[string]any{
			"phase":             "skill_runner",
			"allowed_tools":     policy.AllowedTools,
			"network_allowlist": policy.NetworkAllowlist,
		})
		return runnerResult{Output: result.Output, Session: result.Session}, err
	}
	status = SkillExecutionStatusSucceeded
	return runnerResult{Output: result.Output, Session: result.Session}, err
}

func (r *Runtime) skillShellEnvironment(workspace string, allowedEnv []string) map[string]string {
	env := map[string]string{
		"AGENT_WORKSPACE_DIR": workspace,
	}
	allowed := make(map[string]struct{}, len(allowedEnv))
	for _, key := range allowedEnv {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		allowed[key] = struct{}{}
		if value, ok := os.LookupEnv(key); ok {
			env[key] = value
		}
	}
	if declaresVertexAccessToken(allowed) {
		if token, err := skillShellVertexAccessToken(); err == nil && token != "" {
			for _, key := range []string{"VERTEX_ACCESS_TOKEN", "GOOGLE_OAUTH_ACCESS_TOKEN", "GOOGLE_ACCESS_TOKEN"} {
				if _, ok := allowed[key]; ok {
					env[key] = token
				}
			}
		}
	}
	return env
}

var skillShellVertexAccessToken = func() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func declaresVertexAccessToken(allowed map[string]struct{}) bool {
	for _, key := range []string{"VERTEX_ACCESS_TOKEN", "GOOGLE_OAUTH_ACCESS_TOKEN", "GOOGLE_ACCESS_TOKEN"} {
		if _, ok := allowed[key]; ok {
			return true
		}
	}
	return false
}

func (r *Runtime) skillShellRuntime(workspace, skillRoot string, skill *skills.SkillDefinition, policy SkillRuntimePolicy) skills.PromptShellRuntime {
	if r == nil || !r.config.SkillShellSandbox.dockerEnabled() || skill == nil {
		return nil
	}
	sandbox := applySkillSandboxPolicy(r.config.SkillShellSandbox, policy.Sandbox)
	return NewDockerSkillShellRuntime(sandbox, skill.Shell, workspace, skillRoot, r.skillShellEnvironment(workspace, policy.AllowedEnv), policy.AllowedTools)
}

func runWithTokenStream(ctx context.Context, runner Runner, session *state.Session, prompt string, generated bool, onToken func(string)) (engine.Result, error) {
	if streaming, ok := runner.(StreamingRunner); ok {
		if generated {
			return streaming.RunGeneratedPromptStream(ctx, session, prompt, onToken)
		}
		return streaming.RunStream(ctx, session, prompt, onToken)
	}
	if generated {
		return runner.RunGeneratedPrompt(ctx, session, prompt)
	}
	return runner.Run(ctx, session, prompt)
}

func runWithTokenStreamContent(ctx context.Context, runner Runner, session *state.Session, prompt []publictypes.ContentBlock, generated bool, onToken func(string)) (engine.Result, error) {
	if len(prompt) == 1 && prompt[0].Type == "text" {
		return runWithTokenStream(ctx, runner, session, prompt[0].Text, generated, onToken)
	}
	if generated {
		return runWithTokenStream(ctx, runner, session, promptContentText(prompt), generated, onToken)
	}
	if contentRunner, ok := runner.(ContentRunner); ok {
		return contentRunner.RunContent(ctx, session, prompt)
	}
	return runWithTokenStream(ctx, runner, session, promptContentText(prompt), generated, onToken)
}

func promptContentText(prompt []publictypes.ContentBlock) string {
	parts := make([]string, 0, len(prompt))
	for _, block := range prompt {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (r *Runtime) runnerForScope(scope Scope) Runner {
	if r.engineFactory == nil {
		return nilRunner{}
	}
	scope.WorkingDir = r.sandboxedWorkingDir(scope.UserID, scope.WorkingDir)
	if scope.WorkingDir == "" {
		scope.WorkingDir = filepath.Clean(r.config.DefaultWorkingDir)
	}
	if scope.SkillRoot != "" {
		scope.SkillRoot = filepath.Clean(scope.SkillRoot)
	}
	if scope.Artifacts == nil && r.artifacts != nil && strings.TrimSpace(scope.UserID) != "" && strings.TrimSpace(scope.SessionID) != "" {
		scope.Artifacts = sessionArtifactWriter{
			runtime:   r,
			userID:    scope.UserID,
			sessionID: scope.SessionID,
		}
		scope.ArtifactMaxBytes = r.artifacts.MaxBytes()
	}
	scope.Artifacts = NewArtifactContentTypeWriter(scope.Artifacts, scope.ArtifactTypes)
	return r.engineFactory(scope)
}

type sessionArtifactWriter struct {
	runtime   *Runtime
	userID    string
	sessionID string
}

func (w sessionArtifactWriter) Write(ctx context.Context, filename, contentType string, data []byte) (*Artifact, error) {
	if w.runtime == nil {
		return nil, fmt.Errorf("runtime is not configured")
	}
	return w.runtime.CreateArtifact(ctx, w.userID, w.sessionID, filename, contentType, data)
}

func (r *Runtime) sandboxedWorkingDir(userID, requested string) string {
	if r.config.UserWorkspaceRoot == "" {
		if strings.TrimSpace(requested) != "" && r.config.AllowCustomWorkingDir {
			return filepath.Clean(requested)
		}
		return filepath.Clean(r.config.DefaultWorkingDir)
	}
	userRoot := r.userWorkspace(userID)
	if strings.TrimSpace(requested) == "" {
		return userRoot
	}
	clean := filepath.Clean(requested)
	rel, err := filepath.Rel(userRoot, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return userRoot
	}
	return clean
}

func (r *Runtime) userWorkspace(userID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(userID)))
	return filepath.Join(filepath.Clean(r.config.UserWorkspaceRoot), hex.EncodeToString(sum[:])[:32])
}

func (r *Runtime) start(key string, cancel context.CancelFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shuttingDown {
		return ErrRuntimeShuttingDown
	}
	r.running[key] = cancel
	r.wg.Add(1)
	return nil
}

func (r *Runtime) finish(key string) {
	r.mu.Lock()
	delete(r.running, key)
	r.mu.Unlock()
	r.wg.Done()
}

func (r *Runtime) startJob(jobID string, cancel context.CancelFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shuttingDown {
		return ErrRuntimeShuttingDown
	}
	r.runningJobs[jobID] = cancel
	r.wg.Add(1)
	return nil
}

func (r *Runtime) finishJob(jobID string) {
	r.mu.Lock()
	delete(r.runningJobs, jobID)
	r.mu.Unlock()
	r.wg.Done()
}

func (r *Runtime) subscribeJobEvents(jobID string) (<-chan *JobEvent, func()) {
	if r == nil || r.jobEvents == nil {
		ch := make(chan *JobEvent)
		close(ch)
		return ch, func() {}
	}
	return r.jobEvents.Subscribe(jobID)
}

func (r *Runtime) runningSessionIDs(userID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0)
	for key := range r.running {
		var parts []string
		if err := json.Unmarshal([]byte(key), &parts); err == nil && len(parts) == 2 && parts[0] == userID {
			out = append(out, parts[1])
		}
	}
	return out
}

type runnerResult struct {
	Output  string
	Session *state.Session
}

type jobEventSink struct {
	store  JobStore
	broker *jobEventBroker
	job    *Job
}

func (s *jobEventSink) Send(ctx context.Context, event Event) error {
	if s == nil || s.store == nil || s.job == nil {
		return fmt.Errorf("job event sink is not configured")
	}
	if event.SessionID == "" {
		event.SessionID = s.job.SessionID
	}
	event.JobID = s.job.ID
	record := &JobEvent{
		ID:        NewJobEventID(),
		JobID:     s.job.ID,
		UserID:    s.job.UserID,
		SessionID: s.job.SessionID,
		Type:      event.Type,
		Event:     event,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.AddJobEvent(ctx, record); err != nil {
		return err
	}
	s.broker.Publish(record)
	return nil
}

type nilRunner struct{}

func (nilRunner) Run(context.Context, *state.Session, string) (engine.Result, error) {
	return engine.Result{}, fmt.Errorf("engine factory is required")
}

func (nilRunner) RunGeneratedPrompt(context.Context, *state.Session, string) (engine.Result, error) {
	return engine.Result{}, fmt.Errorf("engine factory is required")
}

func formatSkillList(items []*skills.SkillDefinition) string {
	if len(items) == 0 {
		return "No skills available."
	}
	var out strings.Builder
	out.WriteString("# Available Skills\n\n")
	for _, skill := range items {
		out.WriteString(fmt.Sprintf("- `/%s`", skill.Name))
		if strings.TrimSpace(skill.Description) != "" {
			out.WriteString(": ")
			out.WriteString(skill.Description)
		}
		out.WriteString("\n")
	}
	return out.String()
}

func sessionKey(userID, sessionID string) string {
	key, _ := json.Marshal([]string{userID, sessionID})
	return string(key)
}
