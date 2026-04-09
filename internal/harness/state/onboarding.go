package state

import (
	"os"
	"path/filepath"
	"sync"
)

// OnboardingStep represents a step in the project onboarding process.
type OnboardingStep struct {
	Key           string `json:"key"`
	Text          string `json:"text"`
	IsComplete    bool   `json:"is_complete"`
	IsCompletable bool   `json:"is_completable"`
	IsEnabled     bool   `json:"is_enabled"`
}

// OnboardingManager manages project onboarding state.
type OnboardingManager struct {
	mu          sync.RWMutex
	projectRoot string
	config      *ProjectConfig
}

// NewOnboardingManager creates a new onboarding manager.
func NewOnboardingManager(projectRoot string, config *ProjectConfig) *OnboardingManager {
	return &OnboardingManager{
		projectRoot: projectRoot,
		config:      config,
	}
}

// GetSteps returns the current onboarding steps.
func (m *OnboardingManager) GetSteps() []OnboardingStep {
	hasClaudeMd := m.hasClaudeMd()
	isWorkspaceDirEmpty := m.isWorkspaceDirEmpty()

	return []OnboardingStep{
		{
			Key:           "workspace",
			Text:          "Ask Claude to create a new app or clone a repository",
			IsComplete:    false,
			IsCompletable: true,
			IsEnabled:     isWorkspaceDirEmpty,
		},
		{
			Key:           "claudemd",
			Text:          "Run /init to create a CLAUDE.md file with instructions for Claude",
			IsComplete:    hasClaudeMd,
			IsCompletable: true,
			IsEnabled:     !isWorkspaceDirEmpty,
		},
	}
}

// IsProjectOnboardingComplete checks if all onboarding steps are complete.
func (m *OnboardingManager) IsProjectOnboardingComplete() bool {
	steps := m.GetSteps()
	for _, step := range steps {
		if step.IsCompletable && step.IsEnabled && !step.IsComplete {
			return false
		}
	}
	return true
}

// MaybeMarkProjectOnboardingComplete marks onboarding as complete if all steps are done.
func (m *OnboardingManager) MaybeMarkProjectOnboardingComplete() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Short-circuit if already marked complete
	if m.config.HasCompletedProjectOnboarding {
		return
	}

	if m.IsProjectOnboardingComplete() {
		m.config.HasCompletedProjectOnboarding = true
	}
}

// ShouldShowProjectOnboarding determines if onboarding should be shown.
func (m *OnboardingManager) ShouldShowProjectOnboarding() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Short-circuit on cached config
	if m.config.HasCompletedProjectOnboarding ||
		m.config.ProjectOnboardingSeenCount >= 4 ||
		os.Getenv("IS_DEMO") != "" {
		return false
	}

	return !m.IsProjectOnboardingComplete()
}

// IncrementProjectOnboardingSeenCount increments the seen count.
func (m *OnboardingManager) IncrementProjectOnboardingSeenCount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.ProjectOnboardingSeenCount++
}

// hasClaudeMd checks if CLAUDE.md exists in the project root.
func (m *OnboardingManager) hasClaudeMd() bool {
	claudeMdPath := filepath.Join(m.projectRoot, "CLAUDE.md")
	_, err := os.Stat(claudeMdPath)
	return err == nil
}

// isWorkspaceDirEmpty checks if the workspace directory is empty.
func (m *OnboardingManager) isWorkspaceDirEmpty() bool {
	entries, err := os.ReadDir(m.projectRoot)
	if err != nil {
		return false
	}

	// Consider directory empty if it only contains hidden files
	for _, entry := range entries {
		name := entry.Name()
		if len(name) > 0 && name[0] != '.' {
			return false
		}
	}
	return true
}

// ProjectConfig represents project-specific configuration.
type ProjectConfig struct {
	HasCompletedProjectOnboarding bool `json:"has_completed_project_onboarding"`
	ProjectOnboardingSeenCount    int  `json:"project_onboarding_seen_count"`
}
