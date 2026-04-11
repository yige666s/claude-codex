package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"claude-codex/internal/backend/services/promptsuggestion"
	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/state"
	"claude-codex/internal/harness/tools"
)

type appModel struct {
	title           string
	workingDir      string
	theme           string
	styles          themeStyles
	session         *state.Session
	permissionMode  string
	authStatus      AuthViewData
	sandboxView     SandboxViewData
	mcpView         MCPViewData
	teamsView       TeamsViewData
	skillStats      SkillStatsViewData
	contextBudget   ContextBudgetViewData
	loadSandboxView func() SandboxViewData
	loadMCPView     func() MCPViewData
	loadTeamsView   func() TeamsViewData
	runner          Runner
	generatedRunner GeneratedRunner
	streamRunner    StreamRunner
	saveTheme       SaveTheme
	input           textarea.Model
	output          viewport.Model
	diff            viewport.Model
	sidebar         viewport.Model
	spin            spinner.Model
	busy            bool
	history         []string
	historyIdx      int
	errText         string
	permission      *permissionEnvelope
	broker          *PermissionBroker
	markdown        *glamour.TermRenderer
	lastDiff        string
	lastStatus      string
	parentCtx       context.Context
	// streaming state
	pendingText string
	chunkCh     chan string
	// command suggestions
	suggestions     []CommandSuggestion
	suggestionIndex int
	registry        CommandRegistry
	// tool execution progress
	progressCh         chan tools.ProgressEvent
	currentTool        string
	toolProgress       float64
	toolMessage        string
	promptSuggestion   string
	promptSuggestionCh chan string
	activeTasks        map[string]taskSnapshot
	taskHistory        []taskSnapshot
	notifications      []string
	requestCancel      context.CancelFunc
	requestID          int64
	width              int
	height             int
	overlay            overlayMode
	taskOverlayIndex   int
	taskOverlayDetail  bool
	survey             feedbackSurveyState
}

type resultMsg struct {
	result    engine.Result
	err       error
	requestID int64
}
type permissionMsg struct {
	envelope permissionEnvelope
}
type chunkMsg struct {
	text      string
	requestID int64
}
type toolProgressMsg struct {
	toolName string
	status   string
	progress float64
	message  string
	metadata map[string]string
}
type promptSuggestionMsg struct {
	text string
}

type skillPromptMatcher interface {
	MatchSkillPrompt(prompt string) (string, []string, bool)
}

type taskSnapshot struct {
	Name      string
	Status    string
	Progress  float64
	Message   string
	UpdatedAt time.Time
	History   []string
	Metadata  map[string]string
}

type overlayMode string

const (
	overlayNone    overlayMode = ""
	overlayHelp    overlayMode = "help"
	overlayTasks   overlayMode = "tasks"
	overlayContext overlayMode = "context"
	overlayAuth    overlayMode = "auth"
	overlayCompact overlayMode = "compact"
	overlaySandbox overlayMode = "sandbox"
	overlayMCP     overlayMode = "mcp"
	overlayTeams   overlayMode = "teams"
	overlaySurvey  overlayMode = "survey"
)

type feedbackSurveyStage string

const (
	surveyClosed feedbackSurveyStage = "closed"
	surveyOpen   feedbackSurveyStage = "open"
	surveyThanks feedbackSurveyStage = "thanks"
)

type feedbackSurveyState struct {
	Stage        feedbackSurveyStage
	Prompted     bool
	LastResponse int
	UpdatedAt    time.Time
}

func newModel(options Options) appModel {
	parentCtx := options.Context
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	input := textarea.New()
	input.Placeholder = "Describe what to do"
	input.ShowLineNumbers = false
	input.SetHeight(3)
	input.CharLimit = 10000
	input.Focus()

	output := viewport.New(80, 20)
	diff := viewport.New(80, 6)
	sidebar := viewport.New(28, 20)

	spin := spinner.New()
	spin.Spinner = spinner.Dot

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)

	model := appModel{
		title:              options.Title,
		workingDir:         options.WorkingDir,
		theme:              NormalizeTheme(options.Theme),
		styles:             stylesForTheme(options.Theme),
		session:            options.Session,
		permissionMode:     options.PermissionMode,
		authStatus:         options.AuthStatus,
		skillStats:         options.SkillStats,
		contextBudget:      options.ContextBudget,
		loadSandboxView:    options.LoadSandboxView,
		loadMCPView:        options.LoadMCPView,
		loadTeamsView:      options.LoadTeamsView,
		runner:             options.Runner,
		generatedRunner:    options.GeneratedRunner,
		streamRunner:       options.StreamRunner,
		saveTheme:          options.SaveTheme,
		input:              input,
		output:             output,
		diff:               diff,
		sidebar:            sidebar,
		spin:               spin,
		history:            []string{},
		historyIdx:         -1,
		broker:             options.PermissionBroker,
		markdown:           renderer,
		lastStatus:         "idle",
		parentCtx:          parentCtx,
		registry:           options.Registry,
		suggestions:        []CommandSuggestion{},
		suggestionIndex:    0,
		progressCh:         options.ProgressCh,
		promptSuggestionCh: options.PromptSuggestionCh,
		activeTasks:        map[string]taskSnapshot{},
		taskHistory:        []taskSnapshot{},
		notifications:      []string{},
		width:              120,
		height:             40,
		overlay:            overlayNone,
		taskOverlayIndex:   0,
		taskOverlayDetail:  false,
		survey:             feedbackSurveyState{Stage: surveyClosed},
	}
	model.refreshExternalViews()
	model.refreshViews()
	return model
}

func (m appModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick}
	if m.broker != nil {
		cmds = append(cmds, waitForPermission(m.broker))
	}
	if m.progressCh != nil {
		cmds = append(cmds, waitForProgress(m.progressCh))
	}
	if m.promptSuggestionCh != nil {
		cmds = append(cmds, waitForPromptSuggestion(m.promptSuggestionCh))
	}
	return tea.Batch(cmds...)
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		sidebarWidth := 32
		if msg.Width < 110 {
			sidebarWidth = 24
		}
		if msg.Width < 90 {
			sidebarWidth = 0
		}
		m.output.Width = maxInt(20, msg.Width-sidebarWidth-12)
		m.output.Height = maxInt(8, msg.Height-14)
		m.diff.Width = m.output.Width
		m.sidebar.Width = maxInt(20, sidebarWidth)
		m.sidebar.Height = m.output.Height + 2
		// input textarea fills the same width as the output viewport
		m.input.SetWidth(m.output.Width)
		m.styles.input = m.styles.input.Width(m.output.Width + 2)
		m.refreshViews()
		return m, nil
	case tea.MouseMsg:
		if tea.MouseEvent(msg).IsWheel() {
			if m.isSidebarMouseEvent(msg) {
				switch msg.Button {
				case tea.MouseButtonWheelUp:
					m.sidebar.LineUp(3)
				case tea.MouseButtonWheelDown:
					m.sidebar.LineDown(3)
				}
			} else {
				switch msg.Button {
				case tea.MouseButtonWheelUp:
					m.output.LineUp(3)
				case tea.MouseButtonWheelDown:
					m.output.LineDown(3)
				}
			}
			return m, nil
		}
	case tea.KeyMsg:
		keyString := msg.String()
		if keyString == "" && len(msg.Runes) > 0 {
			keyString = string(msg.Runes)
		}
		if m.permission != nil {
			switch strings.ToLower(keyString) {
			case "y", "enter":
				m.permission.reply <- permissionResult{}
				m.permission = nil
				return m, waitForPermission(m.broker)
			case "n", "esc":
				m.permission.reply <- permissionResult{err: fmt.Errorf("tool %s was denied by the operator", m.permission.request.ToolName)}
				m.permission = nil
				return m, waitForPermission(m.broker)
			default:
				return m, nil
			}
		}
		if m.overlay == overlaySurvey && m.survey.Stage == surveyOpen {
			if rating := surveyRatingFromKey(keyString); rating > 0 {
				m.recordSurveyResponse(rating)
				m.refreshViews()
				return m, nil
			}
		}

		switch keyString {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.busy && m.requestCancel != nil {
				m.requestCancel()
				m.requestCancel = nil
				m.requestID++
				m.busy = false
				m.pendingText = ""
				m.chunkCh = nil
				m.lastStatus = "cancelled"
				m.pushNotification("Request cancelled")
				m.refreshViews()
				return m, nil
			}
			if m.overlay != overlayNone {
				m.overlay = overlayNone
				m.lastStatus = "idle"
				m.refreshViews()
				return m, nil
			}
		case "up":
			if m.overlay == overlayTasks {
				if m.taskOverlayDetail {
					return m, nil
				}
				if m.taskOverlayIndex > 0 {
					m.taskOverlayIndex--
					m.refreshViews()
				}
				return m, nil
			}
			// Navigate suggestions up
			if len(m.suggestions) > 0 && m.suggestionIndex > 0 {
				m.suggestionIndex--
				return m, nil
			}
			if m.historyIdx > 0 {
				m.historyIdx--
				m.input.SetValue(m.history[m.historyIdx])
				m.input.CursorEnd()
				return m, nil
			}
		case "down":
			if m.overlay == overlayTasks {
				if m.taskOverlayDetail {
					return m, nil
				}
				maxIndex := len(m.sortedTasks()) - 1
				if m.taskOverlayIndex < maxIndex {
					m.taskOverlayIndex++
					m.refreshViews()
				}
				return m, nil
			}
			// Navigate suggestions down
			if len(m.suggestions) > 0 && m.suggestionIndex < len(m.suggestions)-1 {
				m.suggestionIndex++
				return m, nil
			}
			if m.historyIdx >= 0 && m.historyIdx < len(m.history)-1 {
				m.historyIdx++
				m.input.SetValue(m.history[m.historyIdx])
				m.input.CursorEnd()
				return m, nil
			}
			if len(m.history) > 0 && m.historyIdx == len(m.history)-1 {
				m.historyIdx = len(m.history)
				m.input.Reset()
				return m, nil
			}
		case "pgup", "ctrl+u":
			// Page up - works in any mode
			m.output.ViewUp()
			return m, nil
		case "pgdown", "ctrl+d":
			// Page down - works in any mode
			m.output.ViewDown()
			return m, nil
		case "home", "ctrl+home":
			// Go to top - works in any mode
			m.output.GotoTop()
			return m, nil
		case "end", "ctrl+end":
			// Go to bottom - works in any mode
			m.output.GotoBottom()
			return m, nil
		case "ctrl+k":
			// Scroll up (vim-style) - works in any mode
			m.output.LineUp(1)
			return m, nil
		case "ctrl+j":
			// Scroll down (vim-style) - works in any mode
			m.output.LineDown(1)
			return m, nil
		case "alt+k":
			// Scroll sidebar up to inspect notices/runtime panels
			m.sidebar.LineUp(1)
			return m, nil
		case "alt+j":
			// Scroll sidebar down to inspect notices/runtime panels
			m.sidebar.LineDown(1)
			return m, nil
		case "ctrl+t":
			m.toggleOverlay(overlayTasks, "tasks")
			return m, nil
		case "ctrl+g":
			m.toggleOverlay(overlayContext, "context")
			return m, nil
		case "ctrl+h":
			m.toggleOverlay(overlayHelp, "help")
			return m, nil
		case "ctrl+a":
			m.toggleOverlay(overlayAuth, "auth")
			return m, nil
		case "ctrl+o":
			m.toggleOverlay(overlayCompact, "compact")
			return m, nil
		case "tab":
			// Tab key for suggestion navigation or theme toggle
			if len(m.suggestions) > 0 {
				// Accept current suggestion
				if m.suggestionIndex < len(m.suggestions) {
					suggestion := m.suggestions[m.suggestionIndex]
					m.input.SetValue(suggestion.Command.Name + " ")
					m.suggestions = []CommandSuggestion{}
					m.suggestionIndex = 0
				}
				return m, nil
			}
			if suggestion := strings.TrimSpace(m.visiblePromptSuggestion()); suggestion != "" && strings.TrimSpace(m.input.Value()) == "" {
				m.input.SetValue(suggestion)
				return m, nil
			}
			// Otherwise toggle theme
			next := "light"
			if m.theme == "light" {
				next = "dark"
			}
			m.theme = next
			m.styles = stylesForTheme(next)
			if m.saveTheme != nil {
				if err := m.saveTheme(next); err != nil {
					m.errText = err.Error()
				}
			}
			m.refreshViews()
			return m, nil
		case "enter":
			if m.overlay == overlayTasks {
				if len(m.sortedTasks()) == 0 {
					return m, nil
				}
				m.taskOverlayDetail = !m.taskOverlayDetail
				m.refreshViews()
				return m, nil
			}
			if !m.busy {
				text := strings.TrimSpace(m.input.Value())
				if text == "" {
					return m, nil
				}

				if matcher, ok := m.registry.(skillPromptMatcher); ok && !strings.HasPrefix(text, "/") {
					if commandName, autoArgs, matched := matcher.MatchSkillPrompt(text); matched {
						m.busy = true
						m.errText = ""
						m.lastStatus = "running"
						m.history = append(m.history, text)
						m.historyIdx = len(m.history)
						m.input.Reset()
						m.promptSuggestion = ""
						m.session.AddUserMessage(text)
						m.refreshViews()
						m.output.GotoBottom()

						output, err := m.registry.Execute(m.parentCtx, commandName, autoArgs)
						if err != nil {
							m.busy = false
							m.errText = err.Error()
							m.lastStatus = "error"
							m.session.AddAssistantMessage(fmt.Sprintf("Error: %s", err.Error()))
							m.refreshViews()
							return m, nil
						}
						if strings.HasPrefix(output, "__SKILL_PROMPT__") {
							skillPrompt := strings.TrimPrefix(output, "__SKILL_PROMPT__")
							runCtx, requestID := m.beginRequest()
							return m, runGeneratedPromptCmd(runCtx, requestID, m.generatedRunner, m.session, skillPrompt)
						}
						m.busy = false
						m.lastStatus = "idle"
						if output != "" {
							m.session.AddAssistantMessage(output)
						}
						m.refreshViews()
						return m, nil
					}
				}

				// Check if this is a slash command
				if strings.HasPrefix(text, "/") && m.registry != nil {
					m.input.Reset()
					m.suggestions = []CommandSuggestion{}
					m.suggestionIndex = 0

					// Add user message to show the command in transcript
					m.session.AddUserMessage(text)

					// Parse command and args
					parts := strings.Fields(text)
					cmdName := parts[0]
					args := []string{}
					if len(parts) > 1 {
						args = parts[1:]
					}

					// Execute the slash command and capture output
					output, err := m.registry.Execute(m.parentCtx, cmdName, args)
					if err != nil {
						m.errText = err.Error()
						m.lastStatus = "error"
						// Add error message to transcript
						m.session.AddAssistantMessage(fmt.Sprintf("Error: %s", err.Error()))
						m.refreshViews()
						return m, nil
					}

					// Check if this is a skill prompt that needs to be sent to AI
					if strings.HasPrefix(output, "__SKILL_PROMPT__") {
						skillPrompt := strings.TrimPrefix(output, "__SKILL_PROMPT__")
						// Execute the generated skill prompt without recording it as
						// a visible user message in the transcript.
						m.busy = true
						m.errText = ""
						m.lastStatus = "running"
						m.input.Reset()
						runCtx, requestID := m.beginRequest()

						// User message already added at line 267, no need to add again
						m.refreshViews()
						m.output.GotoBottom()

						// We deliberately use the non-streaming generated-prompt path
						// here so internal skill prompts don't get re-added as user
						// messages by the engine's normal Run flow.
						return m, runGeneratedPromptCmd(runCtx, requestID, m.generatedRunner, m.session, skillPrompt)
					}

					// Regular command output
					m.lastStatus = "idle"
					if output != "" {
						m.session.AddAssistantMessage(output)
					} else {
						m.session.AddAssistantMessage(fmt.Sprintf("Command %s executed successfully", cmdName))
					}
					m.refreshExternalViews()
					m.refreshViews()
					return m, nil
				}

				m.busy = true
				m.errText = ""
				m.lastStatus = "running"
				m.history = append(m.history, text)
				m.historyIdx = len(m.history)
				m.input.Reset()
				m.promptSuggestion = ""

				// Immediately add user message so it shows in the transcript at once
				m.session.AddUserMessage(text)
				m.refreshViews()
				m.output.GotoBottom()
				runCtx, requestID := m.beginRequest()

				if m.streamRunner != nil {
					ch := make(chan string, 64)
					m.chunkCh = ch
					return m, tea.Batch(
						runStreamCmd(runCtx, requestID, m.streamRunner, m.session, text, ch),
						waitForChunk(ch, requestID),
					)
				}
				return m, runPromptCmd(runCtx, requestID, m.runner, m.session, text)
			}
		case "ctrl+p":
			if m.historyIdx > 0 {
				m.historyIdx--
				m.input.SetValue(m.history[m.historyIdx])
				m.input.CursorEnd()
			}
			return m, nil
		case "ctrl+n":
			if m.historyIdx >= 0 && m.historyIdx < len(m.history)-1 {
				m.historyIdx++
				m.input.SetValue(m.history[m.historyIdx])
				m.input.CursorEnd()
			} else {
				m.historyIdx = len(m.history)
				m.input.Reset()
			}
			return m, nil
		}
	case chunkMsg:
		if msg.requestID != m.requestID {
			return m, nil
		}
		m.pendingText += msg.text
		m.refreshViews()
		m.output.GotoBottom()
		// keep listening for more chunks
		return m, waitForChunk(m.chunkCh, msg.requestID)
	case toolProgressMsg:
		m.currentTool = msg.toolName
		m.toolProgress = msg.progress
		m.toolMessage = msg.message
		m.updateTaskSnapshot(msg)
		switch msg.status {
		case "started":
			m.lastStatus = fmt.Sprintf("▶ %s", msg.toolName)
		case "completed":
			m.lastStatus = fmt.Sprintf("✓ %s", msg.toolName)
			if msg.message != "" {
				m.pushNotification(msg.toolName + ": " + msg.message)
			}
		case "failed":
			m.lastStatus = fmt.Sprintf("✗ %s: %s", msg.toolName, msg.message)
			if msg.message != "" {
				m.pushNotification("Error: " + msg.message)
			}
		}
		m.refreshViews()
		return m, waitForProgress(m.progressCh)
	case resultMsg:
		if msg.requestID != m.requestID {
			return m, nil
		}
		m.busy = false
		m.pendingText = ""
		m.chunkCh = nil
		m.requestCancel = nil
		if errors.Is(msg.err, context.Canceled) {
			m.errText = ""
			m.lastStatus = "cancelled"
			m.refreshViews()
			m.output.GotoBottom()
			return m, nil
		}
		if msg.err != nil {
			m.errText = msg.err.Error()
			m.lastStatus = "error"
			m.pushNotification("Run failed: " + msg.err.Error())
		} else {
			m.session = msg.result.Session
			m.refreshExternalViews()
			m.lastStatus = "idle"
		}
		m.refreshViews()
		m.output.GotoBottom()
		return m, nil
	case promptSuggestionMsg:
		m.promptSuggestion = strings.TrimSpace(msg.text)
		if m.promptSuggestion != "" {
			m.pushNotification("Suggestion ready")
		}
		m.refreshViews()
		return m, waitForPromptSuggestion(m.promptSuggestionCh)
	case permissionMsg:
		m.permission = &msg.envelope
		m.lastStatus = "permission required"
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	if !m.busy {
		m.input, cmd = m.input.Update(msg)
		// Update suggestions when input changes
		m.updateSuggestions()
	}
	return m, cmd
}

func (m appModel) View() string {
	header := m.styles.header.Render(m.title) + "\n" + m.styles.subtle.Render(m.workingDir)
	body := m.output.View()
	sidebarView := m.sidebar.View()

	statusLeft := m.spin.View()
	if !m.busy {
		statusLeft = " "
	}
	status := m.styles.status.Render(strings.TrimSpace(statusLeft + " " + m.lastStatus + "  tokens:" + itoa(m.session.Usage.TotalTokens) + "  cost:$" + formatCost(m.session.Usage.EstimatedCostUSD)))

	// Render suggestions if available
	suggestionsView := ""
	if len(m.suggestions) > 0 {
		suggestionsView = m.renderSuggestions()
	}
	promptSuggestionView := ""
	if suggestion := m.visiblePromptSuggestion(); suggestion != "" {
		promptSuggestionView = m.styles.subtle.Render("Suggestion: " + suggestion)
	}

	mainArea := body
	if sidebarView != "" {
		mainArea = lipgloss.JoinHorizontal(lipgloss.Top, body, "  ", sidebarView)
	}

	extras := []string{}
	if m.permission != nil {
		extras = append(extras, m.styles.modal.Render(renderPermissionDialog(m.permission.request)))
	}
	if modal := m.renderOverlay(); modal != "" {
		extras = append(extras, m.styles.modal.Render(modal))
	}
	if m.errText != "" {
		extras = append(extras, m.styles.errorText.Render(m.errText))
	}

	return m.styles.app.Render(strings.Join([]string{
		header,
		"",
		mainArea,
		strings.Join(extras, "\n\n"),
		"",
		m.styles.input.Render(m.input.View()),
		suggestionsView,
		promptSuggestionView,
		status,
	}, "\n"))
}

func (m *appModel) refreshViews() {
	m.styles = stylesForTheme(m.theme)
	m.output.SetContent(renderTranscript(m.session, m.markdown, m.styles, m.output.Width, m.pendingText))
	m.lastDiff = renderDiffSummary(m.session.LastMessage())
	m.diff.SetContent(m.lastDiff)
	m.sidebar.SetContent(m.renderSidebar())
	m.sidebar.SetYOffset(m.sidebar.YOffset)
}

func (m *appModel) beginRequest() (context.Context, int64) {
	if m.requestCancel != nil {
		m.requestCancel()
		m.requestCancel = nil
	}
	m.requestID++
	ctx, cancel := context.WithCancel(m.parentCtx)
	m.requestCancel = cancel
	return ctx, m.requestID
}

// updateSuggestions updates command suggestions based on current input
func (m *appModel) updateSuggestions() {
	if m.registry == nil {
		return
	}

	input := strings.TrimSpace(m.input.Value())

	// Only show suggestions if input starts with "/"
	if !strings.HasPrefix(input, "/") {
		m.suggestions = []CommandSuggestion{}
		m.suggestionIndex = 0
		return
	}

	// Generate suggestions
	m.suggestions = GenerateCommandSuggestions(input, m.registry, 5)
	m.suggestionIndex = 0
}

// renderSuggestions renders the command suggestions list
func (m appModel) renderSuggestions() string {
	if len(m.suggestions) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, m.styles.subtle.Render("Commands:"))

	for i, suggestion := range m.suggestions {
		prefix := "  "
		style := m.styles.subtle
		if i == m.suggestionIndex {
			prefix = "▶ "
			style = m.styles.assistant
		}

		cmdText := suggestion.Command.Name
		if suggestion.Command.Usage != "" {
			cmdText += " " + suggestion.Command.Usage
		}
		if suggestion.Command.Description != "" {
			cmdText += " - " + suggestion.Command.Description
		}

		lines = append(lines, style.Render(prefix+cmdText))
	}

	return strings.Join(lines, "\n")
}

func (m appModel) renderSidebar() string {
	if m.sidebar.Width <= 0 {
		return ""
	}

	sections := []string{
		m.styles.modal.Width(maxInt(20, m.sidebar.Width-2)).Render(renderContextWorkspacePanel(m.session, m.authStatus, m.permissionMode, m.sandboxView, m.mcpView, m.teamsView, m.survey, m.skillStats, m.contextBudget)),
		m.styles.modal.Width(maxInt(20, m.sidebar.Width-2)).Render(renderTasksPanel(m.sortedTasks(), m.taskHistory)),
	}

	if note := m.renderCompactPanel(); note != "" {
		sections = append(sections, m.styles.modal.Width(maxInt(20, m.sidebar.Width-2)).Render(note))
	}
	if len(m.notifications) > 0 {
		sections = append(sections, m.styles.modal.Width(maxInt(20, m.sidebar.Width-2)).Render(renderNotificationsPanel(m.notifications)))
	}

	return strings.Join(sections, "\n\n")
}

func (m appModel) renderCompactPanel() string {
	for i := len(m.session.Messages) - 1; i >= 0; i-- {
		msg := m.session.Messages[i]
		if msg.Hidden && strings.Contains(msg.Content, "compressed") {
			return "Compact Summary\n\n" + summarizeCompactMessage(msg.Content)
		}
	}
	return ""
}

func (m *appModel) updateTaskSnapshot(msg toolProgressMsg) {
	if msg.toolName == "" {
		return
	}
	snapshot := taskSnapshot{
		Name:      msg.toolName,
		Status:    msg.status,
		Progress:  msg.progress,
		Message:   msg.message,
		UpdatedAt: time.Now(),
		Metadata:  cloneTaskMetadata(msg.metadata),
	}
	if previous, ok := m.activeTasks[msg.toolName]; ok {
		snapshot.History = append(snapshot.History, previous.History...)
		if len(snapshot.Metadata) == 0 {
			snapshot.Metadata = cloneTaskMetadata(previous.Metadata)
		}
	}
	if msg.message != "" {
		snapshot.History = append(snapshot.History, fmt.Sprintf("%s [%s] %s", snapshot.UpdatedAt.Format("15:04:05"), msg.status, msg.message))
		if len(snapshot.History) > 8 {
			snapshot.History = snapshot.History[len(snapshot.History)-8:]
		}
	}
	if msg.status == "completed" || msg.status == "failed" {
		if snapshot.Message == "" {
			snapshot.Message = msg.status
		}
	}
	m.activeTasks[msg.toolName] = snapshot
	m.recordTaskHistory(snapshot)
	if msg.status == "completed" || msg.status == "failed" {
		delete(m.activeTasks, msg.toolName)
	}
}

func (m *appModel) toggleOverlay(mode overlayMode, label string) {
	if m.overlay == mode {
		m.overlay = overlayNone
		m.lastStatus = "idle"
	} else {
		m.refreshOverlayData(mode)
		m.overlay = mode
		m.lastStatus = label + " overlay"
		if mode == overlayTasks {
			m.taskOverlayIndex = 0
			m.taskOverlayDetail = false
		}
		if mode == overlaySurvey {
			m.ensureSurveyOpen()
		}
	}
	m.refreshViews()
}

func (m *appModel) pushNotification(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if len(m.notifications) > 0 && m.notifications[len(m.notifications)-1] == value {
		return
	}
	m.notifications = append(m.notifications, value)
	if len(m.notifications) > 100 {
		m.notifications = m.notifications[len(m.notifications)-100:]
	}
}

func (m appModel) isSidebarMouseEvent(msg tea.MouseMsg) bool {
	if m.sidebar.Width <= 0 {
		return false
	}
	sidebarStart := maxInt(0, m.width-m.sidebar.Width)
	return msg.X >= sidebarStart
}

func (m appModel) renderOverlay() string {
	switch m.overlay {
	case overlayHelp:
		return renderHelpOverlay()
	case overlayTasks:
		return renderTasksOverlayDetail(m.sortedTasks(), m.taskHistory, m.taskOverlayIndex, m.taskOverlayDetail)
	case overlayContext:
		return renderContextOverlayDetail(m.session, m.authStatus, m.skillStats, m.contextBudget, m.permissionMode)
	case overlayAuth:
		return renderAuthOverlay(m.authStatus, m.session)
	case overlayCompact:
		return renderCompactOverlay(m.session)
	case overlaySandbox:
		return renderSandboxOverlay(m.sandboxView)
	case overlayMCP:
		return renderMCPOverlay(m.mcpView)
	case overlayTeams:
		return renderTeamsOverlay(m.teamsView)
	case overlaySurvey:
		return renderSurveyOverlay(m.survey)
	default:
		return ""
	}
}

func (m appModel) sortedTasks() []taskSnapshot {
	items := make([]taskSnapshot, 0, len(m.activeTasks))
	for _, task := range m.activeTasks {
		items = append(items, task)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items
}

func (m *appModel) recordTaskHistory(snapshot taskSnapshot) {
	filtered := make([]taskSnapshot, 0, len(m.taskHistory))
	for _, item := range m.taskHistory {
		if item.Name == snapshot.Name {
			continue
		}
		filtered = append(filtered, item)
	}
	m.taskHistory = append([]taskSnapshot{snapshot}, filtered...)
	if len(m.taskHistory) > 12 {
		m.taskHistory = m.taskHistory[:12]
	}
}

func (m *appModel) refreshExternalViews() {
	if m.loadSandboxView != nil {
		m.sandboxView = m.loadSandboxView()
	}
	if m.loadMCPView != nil {
		m.mcpView = m.loadMCPView()
	}
	if m.loadTeamsView != nil {
		m.teamsView = m.loadTeamsView()
	}
}

func (m *appModel) refreshOverlayData(mode overlayMode) {
	switch mode {
	case overlaySandbox:
		if m.loadSandboxView != nil {
			m.sandboxView = m.loadSandboxView()
		}
	case overlayMCP:
		if m.loadMCPView != nil {
			m.mcpView = m.loadMCPView()
		}
	case overlayTeams:
		if m.loadTeamsView != nil {
			m.teamsView = m.loadTeamsView()
		}
	}
}

func (m *appModel) ensureSurveyOpen() {
	if m.survey.Stage == surveyClosed {
		m.survey.Stage = surveyOpen
	}
	m.survey.Prompted = true
}

func (m *appModel) recordSurveyResponse(rating int) {
	if rating < 1 || rating > 5 {
		return
	}
	m.survey.Stage = surveyThanks
	m.survey.Prompted = true
	m.survey.LastResponse = rating
	m.survey.UpdatedAt = time.Now()
	m.lastStatus = "feedback recorded"
	m.pushNotification(fmt.Sprintf("Feedback saved: %s", surveyRatingLabel(rating)))
}

func surveyRatingFromKey(key string) int {
	switch strings.TrimSpace(key) {
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	case "4":
		return 4
	case "5":
		return 5
	default:
		return 0
	}
}

func surveyRatingLabel(rating int) string {
	switch rating {
	case 1:
		return "unhelpful"
	case 2:
		return "weak"
	case 3:
		return "okay"
	case 4:
		return "good"
	case 5:
		return "excellent"
	default:
		return "unknown"
	}
}

func cloneTaskMetadata(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (m appModel) visiblePromptSuggestion() string {
	if m.promptSuggestion == "" {
		return ""
	}
	if len(m.suggestions) > 0 {
		return ""
	}
	reason := promptsuggestion.GetSuggestionSuppressReason(promptsuggestion.AppState{
		PromptSuggestionEnabled: true,
		PendingSandboxRequest:   m.permission != nil,
		PermissionMode:          m.permissionMode,
	})
	if reason != nil {
		return ""
	}
	return m.promptSuggestion
}

func waitForPermission(broker *PermissionBroker) tea.Cmd {
	if broker == nil {
		return nil
	}
	return func() tea.Msg {
		envelope, ok := <-broker.Requests()
		if !ok {
			return nil
		}
		return permissionMsg{envelope: envelope}
	}
}

func waitForPromptSuggestion(ch <-chan string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		text, ok := <-ch
		if !ok {
			return nil
		}
		return promptSuggestionMsg{text: text}
	}
}

func waitForProgress(ch chan tools.ProgressEvent) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return toolProgressMsg{
			toolName: event.ToolName,
			status:   event.Status,
			progress: event.Progress,
			message:  event.Message,
			metadata: cloneTaskMetadata(event.Metadata),
		}
	}
}

// waitForChunk returns a tea.Cmd that blocks until a chunk arrives on ch.
func waitForChunk(ch chan string, requestID int64) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		text, ok := <-ch
		if !ok {
			return nil
		}
		return chunkMsg{text: text, requestID: requestID}
	}
}

func runPromptCmd(ctx context.Context, requestID int64, runner Runner, session *state.Session, prompt string) tea.Cmd {
	return func() tea.Msg {
		if runner == nil {
			return resultMsg{err: context.Canceled, requestID: requestID}
		}
		result, err := runner(ctx, session, prompt)
		return resultMsg{result: result, err: err, requestID: requestID}
	}
}

func runGeneratedPromptCmd(ctx context.Context, requestID int64, runner GeneratedRunner, session *state.Session, prompt string) tea.Cmd {
	return func() tea.Msg {
		if runner == nil {
			return resultMsg{err: context.Canceled, requestID: requestID}
		}
		result, err := runner(ctx, session, prompt)
		return resultMsg{result: result, err: err, requestID: requestID}
	}
}

// runStreamCmd starts the streaming runner in a goroutine and forwards chunks to ch.
func runStreamCmd(ctx context.Context, requestID int64, runner StreamRunner, session *state.Session, prompt string, ch chan string) tea.Cmd {
	return func() tea.Msg {
		result, err := runner(ctx, session, prompt, func(chunk string) {
			ch <- chunk
		})
		close(ch)
		return resultMsg{result: result, err: err, requestID: requestID}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
