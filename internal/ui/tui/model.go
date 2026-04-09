package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"

	"github.com/ding/claude-code/claude-go/internal/harness/engine"
	"github.com/ding/claude-code/claude-go/internal/harness/state"
	"github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type appModel struct {
	title        string
	workingDir   string
	theme        string
	styles       themeStyles
	session      *state.Session
	runner       Runner
	streamRunner StreamRunner
	saveTheme    SaveTheme
	input        textarea.Model
	output       viewport.Model
	diff         viewport.Model
	spin         spinner.Model
	busy         bool
	history      []string
	historyIdx   int
	vimMode      VimMode
	errText      string
	permission   *permissionEnvelope
	broker       *PermissionBroker
	markdown     *glamour.TermRenderer
	lastDiff     string
	lastStatus   string
	parentCtx    context.Context
	// streaming state
	pendingText string
	chunkCh     chan string
	// command suggestions
	suggestions     []CommandSuggestion
	suggestionIndex int
	registry        CommandRegistry
	// tool execution progress
	progressCh      chan tools.ProgressEvent
	currentTool     string
	toolProgress    float64
	toolMessage     string
}

type resultMsg struct {
	result engine.Result
	err    error
}
type permissionMsg struct {
	envelope permissionEnvelope
}
type chunkMsg struct {
	text string
}
type toolProgressMsg struct {
	toolName string
	status   string
	progress float64
	message  string
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
	input.Focus()

	output := viewport.New(80, 20)
	diff := viewport.New(80, 6)

	spin := spinner.New()
	spin.Spinner = spinner.Dot

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)

	model := appModel{
		title:           options.Title,
		workingDir:      options.WorkingDir,
		theme:           NormalizeTheme(options.Theme),
		styles:          stylesForTheme(options.Theme),
		session:         options.Session,
		runner:          options.Runner,
		streamRunner:    options.StreamRunner,
		saveTheme:       options.SaveTheme,
		input:           input,
		output:          output,
		diff:            diff,
		spin:            spin,
		history:         []string{},
		historyIdx:      -1,
		vimMode:         InsertMode,
		broker:          options.PermissionBroker,
		markdown:        renderer,
		lastStatus:      "idle",
		parentCtx:       parentCtx,
		registry:        options.Registry,
		suggestions:     []CommandSuggestion{},
		suggestionIndex: 0,
		progressCh:      options.ProgressCh,
	}
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
	return tea.Batch(cmds...)
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.output.Width = maxInt(20, msg.Width-8)
		m.output.Height = maxInt(8, msg.Height-14)
		m.diff.Width = m.output.Width
		// input textarea fills the same width as the output viewport
		m.input.SetWidth(m.output.Width)
		m.styles.input = m.styles.input.Width(m.output.Width + 2)
		m.refreshViews()
		return m, nil
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

		switch keyString {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.vimMode = NormalMode
			m.lastStatus = "normal mode"
			m.refreshViews()
			return m, nil
		case "i":
			if m.vimMode == NormalMode {
				m.vimMode = InsertMode
				m.lastStatus = "insert mode"
				m.refreshViews()
				return m, nil
			}
		case "up":
			if m.vimMode == NormalMode {
				m.output.LineUp(3)
				return m, nil
			}
			// Navigate suggestions up
			if len(m.suggestions) > 0 && m.suggestionIndex > 0 {
				m.suggestionIndex--
				return m, nil
			}
		case "down":
			if m.vimMode == NormalMode {
				m.output.LineDown(3)
				return m, nil
			}
			// Navigate suggestions down
			if len(m.suggestions) > 0 && m.suggestionIndex < len(m.suggestions)-1 {
				m.suggestionIndex++
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
			if m.vimMode == InsertMode && !m.busy {
				text := strings.TrimSpace(m.input.Value())
				if text == "" {
					return m, nil
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
						// Send the skill prompt to AI as if user typed it
						m.busy = true
						m.errText = ""
						m.lastStatus = "running"
						m.input.Reset()

						// User message already added at line 267, no need to add again
						m.refreshViews()
						m.output.GotoBottom()

						// Run the skill prompt through AI
						if m.streamRunner != nil {
							ch := make(chan string, 64)
							m.chunkCh = ch
							return m, tea.Batch(
								runStreamCmd(m.parentCtx, m.streamRunner, m.session, skillPrompt, ch),
								waitForChunk(ch),
							)
						}
						return m, runPromptCmd(m.parentCtx, m.runner, m.session, skillPrompt)
					}

					// Regular command output
					m.lastStatus = "idle"
					if output != "" {
						m.session.AddAssistantMessage(output)
					} else {
						m.session.AddAssistantMessage(fmt.Sprintf("Command %s executed successfully", cmdName))
					}
					m.refreshViews()
					return m, nil
				}

				m.busy = true
				m.errText = ""
				m.lastStatus = "running"
				m.history = append(m.history, text)
				m.historyIdx = len(m.history)
				m.input.Reset()

				// Immediately add user message so it shows in the transcript at once
				m.session.AddUserMessage(text)
				m.refreshViews()
				m.output.GotoBottom()

				if m.streamRunner != nil {
					ch := make(chan string, 64)
					m.chunkCh = ch
					return m, tea.Batch(
						runStreamCmd(m.parentCtx, m.streamRunner, m.session, text, ch),
						waitForChunk(ch),
					)
				}
				return m, runPromptCmd(m.parentCtx, m.runner, m.session, text)
			}
		case "ctrl+p":
			if m.historyIdx > 0 {
				m.historyIdx--
				m.input.SetValue(m.history[m.historyIdx])
			}
			return m, nil
		case "ctrl+n":
			if m.historyIdx >= 0 && m.historyIdx < len(m.history)-1 {
				m.historyIdx++
				m.input.SetValue(m.history[m.historyIdx])
			} else {
				m.historyIdx = len(m.history)
				m.input.Reset()
			}
			return m, nil
		}
	case chunkMsg:
		m.pendingText += msg.text
		m.refreshViews()
		m.output.GotoBottom()
		// keep listening for more chunks
		return m, waitForChunk(m.chunkCh)
	case toolProgressMsg:
		m.currentTool = msg.toolName
		m.toolProgress = msg.progress
		m.toolMessage = msg.message
		switch msg.status {
		case "started":
			m.lastStatus = fmt.Sprintf("▶ %s", msg.toolName)
		case "completed":
			m.lastStatus = fmt.Sprintf("✓ %s", msg.toolName)
		case "failed":
			m.lastStatus = fmt.Sprintf("✗ %s: %s", msg.toolName, msg.message)
		}
		m.refreshViews()
		return m, waitForProgress(m.progressCh)
	case resultMsg:
		m.busy = false
		m.pendingText = ""
		m.chunkCh = nil
		if msg.err != nil {
			m.errText = msg.err.Error()
			m.lastStatus = "error"
		} else {
			m.session = msg.result.Session
			m.lastStatus = "idle"
		}
		m.refreshViews()
		m.output.GotoBottom()
		return m, nil
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
	if m.vimMode == InsertMode && !m.busy {
		m.input, cmd = m.input.Update(msg)
		// Update suggestions when input changes
		m.updateSuggestions()
	}
	return m, cmd
}

func (m appModel) View() string {
	header := m.styles.header.Render(m.title) + "\n" + m.styles.subtle.Render(m.workingDir)
	body := m.output.View()
	if m.permission != nil {
		body += "\n\n" + m.styles.modal.Render("Permission\n\nAllow "+m.permission.request.ToolName+" with "+string(m.permission.request.Level)+" permission?\n\n[y] allow  [n] deny")
	}
	if strings.TrimSpace(m.lastDiff) != "" {
		body += "\n\n" + m.styles.diff.Render(m.lastDiff)
	}
	if m.errText != "" {
		body += "\n\n" + m.styles.errorText.Render(m.errText)
	}

	statusLeft := m.spin.View()
	if !m.busy {
		statusLeft = " "
	}
	status := m.styles.status.Render(strings.TrimSpace(statusLeft + " " + m.lastStatus + "  " + string(m.vimMode) + "  tokens:" + itoa(m.session.Usage.TotalTokens) + "  cost:$" + formatCost(m.session.Usage.EstimatedCostUSD)))

	// Render suggestions if available
	suggestionsView := ""
	if len(m.suggestions) > 0 {
		suggestionsView = m.renderSuggestions()
	}

	return m.styles.app.Render(strings.Join([]string{
		header,
		"",
		body,
		"",
		m.styles.input.Render(m.input.View()),
		suggestionsView,
		status,
	}, "\n"))
}

func (m *appModel) refreshViews() {
	m.styles = stylesForTheme(m.theme)
	m.output.SetContent(renderTranscript(m.session, m.markdown, m.styles, m.output.Width, m.pendingText))
	m.lastDiff = renderDiffSummary(m.session.LastMessage())
	m.diff.SetContent(m.lastDiff)
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
		}
	}
}

// waitForChunk returns a tea.Cmd that blocks until a chunk arrives on ch.
func waitForChunk(ch chan string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		text, ok := <-ch
		if !ok {
			return nil
		}
		return chunkMsg{text: text}
	}
}

func runPromptCmd(ctx context.Context, runner Runner, session *state.Session, prompt string) tea.Cmd {
	return func() tea.Msg {
		if runner == nil {
			return resultMsg{err: context.Canceled}
		}
		result, err := runner(ctx, session, prompt)
		return resultMsg{result: result, err: err}
	}
}

// runStreamCmd starts the streaming runner in a goroutine and forwards chunks to ch.
func runStreamCmd(ctx context.Context, runner StreamRunner, session *state.Session, prompt string, ch chan string) tea.Cmd {
	return func() tea.Msg {
		result, err := runner(ctx, session, prompt, func(chunk string) {
			ch <- chunk
		})
		close(ch)
		return resultMsg{result: result, err: err}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
