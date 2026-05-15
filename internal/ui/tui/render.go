package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/state"
)

func renderTranscript(session *state.Session, renderer *glamour.TermRenderer, styles themeStyles, width int, pendingText string) string {
	if session == nil {
		return "No messages yet."
	}

	blockWidth := width
	if blockWidth < 20 {
		blockWidth = 20
	}

	parts := make([]string, 0, len(session.Messages)+1)
	lastRole := ""
	group := transcriptAssistantGroup{seenToolCalls: map[string]bool{}}
	flushGroup := func() {
		if block := renderAssistantGroup(group, renderer, styles, blockWidth); block != "" {
			parts = append(parts, block)
			lastRole = "assistant"
		}
		group = transcriptAssistantGroup{seenToolCalls: map[string]bool{}}
	}

	for _, message := range session.Messages {
		if message.Hidden {
			if message.Role == "tool" {
				group.consumeToolMessage(message)
			}
			continue
		}
		switch message.Role {
		case "user":
			flushGroup()
			block := styles.userBlock.Width(blockWidth).Render(
				styles.user.Render("User") + "\n" + message.Content,
			)
			parts = append(parts, block)
			lastRole = "user"
		case "assistant":
			group.consumeAssistantMessage(message)
		case "tool":
			group.consumeToolMessage(message)
		default:
			flushGroup()
			parts = append(parts, message.Content)
			lastRole = message.Role
		}
	}
	flushGroup()

	// Show in-progress streaming assistant block with a blinking cursor indicator
	if pendingText != "" {
		block := styles.assistantBlock.Width(blockWidth).Render(
			styles.assistant.Bold(true).Render("Assistant") + "\n" + pendingText + "▌",
		)
		parts = append(parts, block)
	} else if lastRole == "assistant" && len(parts) > 0 {
		// Add completion indicator after assistant's final message
		parts = append(parts, styles.subtle.Render("✓ Done"))
	}

	if len(parts) == 0 {
		return "No messages yet."
	}
	return strings.Join(parts, "\n")
}

type transcriptAssistantGroup struct {
	toolLines      []string
	finalContent   string
	lastToolOutput string
	seenToolCalls  map[string]bool
}

func (g *transcriptAssistantGroup) consumeAssistantMessage(message state.Message) {
	for _, call := range message.ToolCalls {
		if call.ID != "" {
			g.seenToolCalls[call.ID] = true
		}
		if line := summarizeTranscriptToolCall(call.Name, call.Input); line != "" {
			g.toolLines = append(g.toolLines, line)
		}
	}

	content := strings.TrimSpace(message.Content)
	if content == "" {
		return
	}
	if g.lastToolOutput != "" && content == g.lastToolOutput {
		content = summarizeToolEcho(content, 2)
	}
	g.finalContent = content
}

func (g *transcriptAssistantGroup) consumeToolMessage(message state.Message) {
	if message.ToolCallID != "" && g.seenToolCalls[message.ToolCallID] {
		g.lastToolOutput = strings.TrimSpace(message.ToolOutput)
		return
	}
	if line := summarizeTranscriptToolCall(message.ToolName, message.ToolInput); line != "" {
		g.toolLines = append(g.toolLines, line)
	}
	g.lastToolOutput = strings.TrimSpace(message.ToolOutput)
}

func renderAssistantGroup(group transcriptAssistantGroup, renderer *glamour.TermRenderer, styles themeStyles, blockWidth int) string {
	if len(group.toolLines) == 0 && strings.TrimSpace(group.finalContent) == "" {
		return ""
	}
	lines := make([]string, 0, len(group.toolLines)+2)
	lines = append(lines, styles.assistant.Bold(true).Render("Assistant"))
	for _, line := range group.toolLines {
		lines = append(lines, styles.subtle.Render(line))
	}
	content := strings.TrimSpace(group.finalContent)
	if content != "" {
		if renderer != nil {
			rendered, err := renderer.Render(content)
			if err == nil {
				content = strings.TrimSpace(rendered)
			}
		}
		lines = append(lines, content)
	}
	return styles.assistantBlock.Width(blockWidth).Render(strings.Join(lines, "\n"))
}

func summarizeTranscriptToolCall(toolName string, input json.RawMessage) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return ""
	}

	var payload map[string]any
	_ = json.Unmarshal(input, &payload)

	switch toolName {
	case "bash":
		if command, ok := payload["command"].(string); ok && strings.TrimSpace(command) != "" {
			return "🔧 bash: " + summarizeInline(command, 80)
		}
	case "file_read", "file_write", "file_edit":
		if path, ok := payload["path"].(string); ok && strings.TrimSpace(path) != "" {
			return "🔧 " + toolName + ": " + path
		}
	case "web_fetch":
		if url, ok := payload["url"].(string); ok && strings.TrimSpace(url) != "" {
			return "🔧 web_fetch: " + summarizeInline(url, 80)
		}
	case "agent":
		if description, ok := payload["description"].(string); ok && strings.TrimSpace(description) != "" {
			return "🔧 agent: " + summarizeInline(description, 80)
		}
		if prompt, ok := payload["prompt"].(string); ok && strings.TrimSpace(prompt) != "" {
			return "🔧 agent: " + summarizeInline(prompt, 80)
		}
	}
	return "🔧 " + toolName
}

func summarizeInline(value string, limit int) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func itoa(value int) string {
	return fmt.Sprintf("%d", value)
}

func formatCost(value float64) string {
	return fmt.Sprintf("%.6f", value)
}

func renderBlock(style lipgloss.Style, width int, content string) string {
	if width > 0 {
		return style.Width(width).Render(content)
	}
	return style.Render(content)
}

func renderContextPanel(session *state.Session, authStatus AuthViewData) string {
	if session == nil {
		return "Context\n\nNo session"
	}

	toolCount := countToolMessages(session)
	readCount := countToolReads(session)
	writeCount := countToolWrites(session)

	lines := []string{
		"Context",
		"",
		fmt.Sprintf("Session: %s", session.ID),
		fmt.Sprintf("Messages: %d", len(session.Messages)),
		fmt.Sprintf("Tokens: %s", itoa(session.Usage.TotalTokens)),
		fmt.Sprintf("Tool calls: %d", toolCount),
		fmt.Sprintf("Reads/Writes: %d/%d", readCount, writeCount),
	}
	if authStatus.Status != "" {
		lines = append(lines, fmt.Sprintf("Auth: %s", authStatus.Status))
	}
	return strings.Join(lines, "\n")
}

func countToolMessages(session *state.Session) int {
	count := 0
	for _, message := range session.Messages {
		if message.Role == "tool" {
			count++
		}
	}
	return count
}

func countToolReads(session *state.Session) int {
	count := 0
	for _, message := range session.Messages {
		if message.Role == "tool" && message.ToolName == "file_read" {
			count++
		}
	}
	return count
}

func countToolWrites(session *state.Session) int {
	count := 0
	for _, message := range session.Messages {
		if message.Role == "tool" && (message.ToolName == "file_write" || message.ToolName == "file_edit") {
			count++
		}
	}
	return count
}

func renderTasksPanel(active []taskSnapshot, history []taskSnapshot) string {
	lines := []string{"Runtime", ""}
	if len(active) == 0 && len(history) == 0 {
		lines = append(lines, "No active tasks")
		return strings.Join(lines, "\n")
	}

	for _, task := range active {
		progress := ""
		if task.Progress > 0 {
			progress = fmt.Sprintf(" %.0f%%", task.Progress*100)
		}
		line := fmt.Sprintf("- %s [%s]%s", task.Name, task.Status, progress)
		lines = append(lines, line)
		if task.Message != "" {
			lines = append(lines, "  "+snippet(task.Message))
		}
	}
	if len(history) > 0 {
		lines = append(lines, "", "Recent:")
		limit := minInt(3, len(history))
		for _, task := range history[:limit] {
			lines = append(lines, fmt.Sprintf("- %s [%s]", task.Name, task.Status))
		}
	}
	return strings.Join(lines, "\n")
}

func renderNotificationsPanel(notifications []string) string {
	lines := []string{"Notices", ""}
	if len(notifications) > 10 {
		notifications = notifications[len(notifications)-10:]
	}
	for _, notification := range notifications {
		lines = append(lines, "- "+notification)
	}
	return strings.Join(lines, "\n")
}

func summarizeToolEcho(content string, maxLines int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return content
	}
	if maxLines <= 0 {
		maxLines = 2
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}
	return strings.Join(lines[:maxLines], "\n") + "\n... output truncated ..."
}

func summarizeCompactMessage(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= 180 {
		return content
	}
	return content[:177] + "..."
}

func renderHelpOverlay() string {
	lines := []string{
		"Help",
		"",
		"Core shortcuts:",
		"- ctrl+h: toggle help overlay",
		"- ctrl+t: toggle runtime/tasks overlay",
		"- ctrl+g: toggle context overlay",
		"- ctrl+o: toggle compact overlay",
		"- ctrl+a: toggle auth overlay",
		"- ctrl+o: toggle compact overlay",
		"- tab: accept slash suggestion or prompt suggestion",
		"- esc: cancel request or close overlay",
		"- ctrl+j / ctrl+k: scroll transcript",
		"- alt+j / alt+k: scroll sidebar panels",
		"- use ctrl+... shortcuts for overlays; letter keys stay available for typing",
		"",
		"Panels:",
		"- Sidebar shows workspace, context, runtime, compact, notices",
		"- Modal overlays provide expanded detail",
	}
	return strings.Join(lines, "\n")
}

func renderContextWorkspacePanel(session *state.Session, authStatus AuthViewData, permissionMode string, sandbox SandboxViewData, mcp MCPViewData, teams TeamsViewData, survey feedbackSurveyState, skillStats SkillStatsViewData, budget ContextBudgetViewData) string {
	lines := []string{"Context"}
	if session == nil {
		lines = append(lines, "No session")
		return strings.Join(lines, "\n")
	}
	estimatedTokens := session.EstimateTokens()
	lines = append(lines,
		fmt.Sprintf("ID: %s", abbreviateIdentifier(session.ID, 14)),
		fmt.Sprintf("Msgs/Tok/Tools: %d/%d/%d", len(session.Messages), session.Usage.TotalTokens, countToolMessages(session)),
		fmt.Sprintf("Reads/Writes: %d/%d", countToolReads(session), countToolWrites(session)),
	)
	if skillStats.Total > 0 {
		lines = append(lines, fmt.Sprintf("Skills: %d", skillStats.Total))
	}
	if authStatus.Status != "" {
		lines = append(lines, "Auth: "+authStatus.Status)
	}
	lines = append(lines,
		"Mode: "+permissionMode,
		"Exec: "+fallbackValue(sandbox.ExecutionEnv, "unknown"),
		fmt.Sprintf("MCP/Teams: %d/%d", len(mcp.Servers), len(teams.Teams)),
	)
	if budget.CompressionSoftTokens > 0 {
		lines = append(lines,
			fmt.Sprintf("Ctx used: %d", estimatedTokens),
		)
	}
	switch survey.Stage {
	case surveyOpen:
		lines = append(lines, "Survey: pending")
	case surveyThanks:
		lines = append(lines, "Survey: "+surveyRatingLabel(survey.LastResponse))
	default:
		lines = append(lines, "Survey: idle")
	}
	return strings.Join(lines, "\n")
}

func renderTasksOverlayDetail(tasks []taskSnapshot, history []taskSnapshot, selectedIndex int, showDetail bool) string {
	lines := []string{
		"Runtime Tasks",
		"",
	}
	if len(tasks) == 0 && len(history) == 0 {
		lines = append(lines, "No active runtime tasks.")
		return strings.Join(lines, "\n")
	}

	combined := append([]taskSnapshot{}, tasks...)
	combined = append(combined, history...)
	tasks = dedupeTasks(combined)

	if selectedIndex < 0 {
		selectedIndex = 0
	}
	if selectedIndex >= len(tasks) {
		selectedIndex = len(tasks) - 1
	}

	if !showDetail {
		lines = append(lines, "Use ↑/↓ to select, Enter for detail, Esc to close.", "")
		for i, task := range tasks {
			prefix := "  "
			if i == selectedIndex {
				prefix = "▶ "
			}
			progress := ""
			if task.Progress > 0 {
				progress = fmt.Sprintf(" %.0f%%", task.Progress*100)
			}
			lines = append(lines, fmt.Sprintf("%s%s [%s]%s", prefix, task.Name, task.Status, progress))
		}
		return strings.Join(lines, "\n")
	}

	task := tasks[selectedIndex]
	return renderTaskOverlayDetail(task)
}

func renderTaskOverlayDetail(task taskSnapshot) string {
	switch {
	case task.Name == "bash":
		return renderShellTaskDetail(task)
	case task.Name == "agent":
		return renderAgentTaskDetail(task)
	case strings.HasPrefix(task.Name, "mcp__"):
		return renderMCPTaskDetail(task)
	case strings.HasPrefix(task.Name, "team_"):
		return renderTeamTaskDetail(task)
	default:
		return renderGenericTaskDetail(task)
	}
}

func renderGenericTaskDetail(task taskSnapshot) string {
	lines := []string{
		"Task Detail",
		"",
		"Enter to return to list.",
		"",
		fmt.Sprintf("Name: %s", task.Name),
		fmt.Sprintf("Status: %s", task.Status),
	}
	return appendTaskDetailBody(lines, task)
}

func renderShellTaskDetail(task taskSnapshot) string {
	lines := []string{
		"Shell Task Detail",
		"",
		"Enter to return to list.",
		"",
		fmt.Sprintf("Command: %s", fallbackValue(task.Metadata["command"], task.Message)),
		fmt.Sprintf("Status: %s", task.Status),
	}
	if access := task.Metadata["access"]; access != "" {
		lines = append(lines, "Access: "+access)
	}
	if risk := task.Metadata["risk"]; risk != "" {
		lines = append(lines, "Risk: "+risk)
	}
	return appendTaskDetailBody(lines, task)
}

func renderAgentTaskDetail(task taskSnapshot) string {
	lines := []string{
		"Agent Task Detail",
		"",
		"Enter to return to list.",
		"",
		fmt.Sprintf("Summary: %s", fallbackValue(task.Message, task.Name)),
		fmt.Sprintf("Status: %s", task.Status),
	}
	if description := task.Metadata["description"]; description != "" {
		lines = append(lines, "Task: "+description)
	}
	if teamName := task.Metadata["team_name"]; teamName != "" {
		lines = append(lines, "Team: "+teamName)
	}
	if subagentType := task.Metadata["subagent_type"]; subagentType != "" {
		lines = append(lines, "Subagent type: "+subagentType)
	}
	if model := task.Metadata["model"]; model != "" {
		lines = append(lines, "Model: "+model)
	}
	return appendTaskDetailBody(lines, task)
}

func renderMCPTaskDetail(task taskSnapshot) string {
	lines := []string{
		"MCP Task Detail",
		"",
		"Enter to return to list.",
		"",
		fmt.Sprintf("Server: %s", fallbackValue(task.Metadata["server"], "unknown")),
		fmt.Sprintf("Tool: %s", fallbackValue(task.Metadata["mcp_tool"], task.Name)),
		fmt.Sprintf("Status: %s", task.Status),
	}
	if uri := task.Metadata["uri"]; uri != "" {
		lines = append(lines, "URI: "+uri)
	}
	if path := task.Metadata["path"]; path != "" {
		lines = append(lines, "Path: "+path)
	}
	if url := task.Metadata["url"]; url != "" {
		lines = append(lines, "URL: "+url)
	}
	return appendTaskDetailBody(lines, task)
}

func renderTeamTaskDetail(task taskSnapshot) string {
	lines := []string{
		"Team Task Detail",
		"",
		"Enter to return to list.",
		"",
		fmt.Sprintf("Operation: %s", fallbackValue(task.Metadata["operation"], task.Name)),
		fmt.Sprintf("Team: %s", fallbackValue(task.Metadata["team_name"], "unknown")),
		fmt.Sprintf("Status: %s", task.Status),
	}
	return appendTaskDetailBody(lines, task)
}

func appendTaskDetailBody(lines []string, task taskSnapshot) string {
	if task.Progress > 0 {
		lines = append(lines, fmt.Sprintf("Progress: %.0f%%", task.Progress*100))
	}
	if task.Message != "" {
		lines = append(lines, "Detail:")
		lines = append(lines, task.Message)
	}
	if len(task.History) > 0 {
		lines = append(lines, "", "Recent activity:")
		for _, entry := range task.History {
			lines = append(lines, "- "+entry)
		}
	}
	lines = append(lines, fmt.Sprintf("Updated: %s", task.UpdatedAt.Format(time.RFC3339)))
	return strings.Join(lines, "\n")
}

func renderContextOverlayDetail(session *state.Session, authStatus AuthViewData, skillStats SkillStatsViewData, budget ContextBudgetViewData, permissionMode string) string {
	if session == nil {
		return "Context\n\nNo active session."
	}

	visibleCount := 0
	hiddenCount := 0
	systemCount := 0
	toolBreakdown := map[string]int{}
	for _, msg := range session.Messages {
		if msg.Hidden && msg.Role != "tool" {
			hiddenCount++
		} else if !msg.Hidden {
			visibleCount++
		}
		if msg.Hidden && msg.Role == "user" {
			systemCount++
		}
		if msg.Role == "tool" {
			toolBreakdown[msg.ToolName]++
		}
	}

	lines := []string{
		"Context Detail",
		"",
		fmt.Sprintf("Session ID: %s", session.ID),
		fmt.Sprintf("Working dir: %s", session.WorkingDir),
		fmt.Sprintf("Started: %s", session.StartedAt.Format(time.RFC3339)),
		fmt.Sprintf("Updated: %s", session.UpdatedAt.Format(time.RFC3339)),
		fmt.Sprintf("Messages: %d", len(session.Messages)),
		fmt.Sprintf("Visible messages: %d", visibleCount),
		fmt.Sprintf("Hidden messages: %d", hiddenCount),
		fmt.Sprintf("Hidden system context: %d", systemCount),
		fmt.Sprintf("Input tokens: %d", session.Usage.InputTokens),
		fmt.Sprintf("Output tokens: %d", session.Usage.OutputTokens),
		fmt.Sprintf("Total tokens: %d", session.Usage.TotalTokens),
		fmt.Sprintf("Estimated context tokens: %d", session.EstimateTokens()),
	}
	if authStatus.Status != "" {
		lines = append(lines, fmt.Sprintf("Auth status: %s", authStatus.Status))
	}
	if skillStats.Total > 0 {
		lines = append(lines,
			fmt.Sprintf("Skills loaded: %d", skillStats.Total),
			fmt.Sprintf("User invocable skills: %d", skillStats.UserInvocable),
		)
		if skillStats.Dynamic > 0 || skillStats.Conditional > 0 {
			lines = append(lines, fmt.Sprintf("Dynamic/conditional skills: %d/%d", skillStats.Dynamic, skillStats.Conditional))
		}
	}
	if budget.ContextWindowTokens > 0 || budget.CompressionSoftTokens > 0 {
		estimated := session.EstimateTokens()
		remainingWindow := maxInt(0, budget.ContextWindowTokens-estimated)
		remainingToCompression := maxInt(0, budget.CompressionSoftTokens-estimated)
		lines = append(lines, "",
			fmt.Sprintf("Permission mode: %s", permissionMode),
			fmt.Sprintf("Model: %s", fallbackValue(budget.Model, "unknown")),
			fmt.Sprintf("Context window: %d", budget.ContextWindowTokens),
			fmt.Sprintf("Compression soft limit: %d", budget.CompressionSoftTokens),
			fmt.Sprintf("Compression target: %d", budget.CompressionTargetTokens),
			fmt.Sprintf("Remaining before compress: %d", remainingToCompression),
			fmt.Sprintf("Remaining window: %d", remainingWindow),
		)
	}

	lastUser := strings.TrimSpace(session.LastUserMessage())
	if lastUser != "" {
		lines = append(lines, "", "Last user message:", lastUser)
	}

	if compact := latestCompactMarker(session); compact != "" {
		lines = append(lines, "", "Compact marker:", compact)
	}
	if len(toolBreakdown) > 0 {
		lines = append(lines, "", "Tool breakdown:")
		keys := make([]string, 0, len(toolBreakdown))
		for key := range toolBreakdown {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("- %s: %d", key, toolBreakdown[key]))
		}
	}

	return strings.Join(lines, "\n")
}

func renderAuthOverlay(authStatus AuthViewData, session *state.Session) string {
	lines := []string{
		"Auth",
		"",
	}
	if authStatus.Status == "" {
		authStatus.Status = "unknown"
	}
	lines = append(lines, "Current status: "+authStatus.Status)
	lines = append(lines, fmt.Sprintf("Authenticated: %t", authStatus.Authenticated))
	lines = append(lines, fmt.Sprintf("Trusted device: %t", authStatus.HasTrustedDevice))
	if authStatus.ExpiresAt != "" {
		lines = append(lines, "Expires at: "+authStatus.ExpiresAt)
	}
	if len(authStatus.Scopes) > 0 {
		lines = append(lines, "Scopes: "+strings.Join(authStatus.Scopes, ", "))
	}
	if authStatus.SubscriptionType != "" {
		lines = append(lines, "Subscription: "+authStatus.SubscriptionType)
	}
	if authStatus.RateLimitTier != "" {
		lines = append(lines, "Rate limit tier: "+authStatus.RateLimitTier)
	}
	lines = append(lines, "")
	lines = append(lines, "Related commands:")
	lines = append(lines, "- /login")
	lines = append(lines, "- /logout")
	lines = append(lines, "- /model")
	if session != nil {
		lines = append(lines, "", "Session:", session.ID)
	}
	return strings.Join(lines, "\n")
}

func renderCompactOverlay(session *state.Session) string {
	lines := []string{
		"Compact / Summary",
		"",
	}
	if session == nil {
		lines = append(lines, "No active session.")
		return strings.Join(lines, "\n")
	}

	compact := latestCompactMarker(session)
	if compact == "" {
		lines = append(lines, "No compact marker in the current session.")
	} else {
		lines = append(lines, "Latest marker:")
		lines = append(lines, compact)
	}

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Visible messages: %d", visibleMessageCount(session)))
	lines = append(lines, fmt.Sprintf("Hidden messages: %d", hiddenMessageCount(session)))
	lines = append(lines, fmt.Sprintf("Total tokens: %d", session.Usage.TotalTokens))
	return strings.Join(lines, "\n")
}

func renderSandboxOverlay(view SandboxViewData) string {
	lines := []string{
		"Sandbox / Permissions",
		"",
		fmt.Sprintf("Execution env: %s", fallbackValue(view.ExecutionEnv, "unknown")),
		fmt.Sprintf("Mode: %s", fallbackValue(view.Mode, "default")),
		fmt.Sprintf("Working dir: %s", fallbackValue(view.WorkingDir, ".")),
		fmt.Sprintf("Approval policy: %s", fallbackValue(view.ApprovalPolicy, "not available")),
	}
	if len(view.WritableRoots) > 0 {
		lines = append(lines, "Writable roots:")
		for _, root := range view.WritableRoots {
			lines = append(lines, "- "+root)
		}
	}
	if len(view.Notes) > 0 {
		lines = append(lines, "", "Notes:")
		for _, note := range view.Notes {
			lines = append(lines, "- "+note)
		}
	}
	return strings.Join(lines, "\n")
}

func renderMCPOverlay(view MCPViewData) string {
	lines := []string{
		"MCP Settings",
		"",
	}
	if len(view.Servers) == 0 {
		lines = append(lines, "No MCP servers configured.")
		return strings.Join(lines, "\n")
	}
	for _, server := range view.Servers {
		lines = append(lines, fmt.Sprintf("- %s [%s]", server.Name, server.Transport))
		if server.Target != "" {
			lines = append(lines, "  "+server.Target)
		}
		if server.Source != "" {
			lines = append(lines, "  source: "+server.Source)
		}
	}
	if view.LoadedAt != "" {
		lines = append(lines, "", "Loaded: "+view.LoadedAt)
	}
	return strings.Join(lines, "\n")
}

func renderTeamsOverlay(view TeamsViewData) string {
	lines := []string{
		"Teams",
		"",
	}
	if len(view.Teams) == 0 {
		lines = append(lines, "No persisted teams found.")
		return strings.Join(lines, "\n")
	}
	for _, team := range view.Teams {
		lines = append(lines, fmt.Sprintf("- %s [%s]", team.Name, fallbackValue(team.Source, "unknown")))
		if team.Description != "" {
			lines = append(lines, "  "+team.Description)
		}
		lines = append(lines, fmt.Sprintf("  members: %d  pending permissions: %d", len(team.Members), team.PendingPermissions))
		for _, member := range team.Members {
			mode := fallbackValue(member.Mode, "default")
			status := "inactive"
			if member.Active {
				status = "active"
			}
			lines = append(lines, fmt.Sprintf("  - %s [%s, %s, %s]", member.Name, fallbackValue(member.AgentType, "agent"), mode, status))
		}
	}
	if view.LoadedAt != "" {
		lines = append(lines, "", "Loaded: "+view.LoadedAt)
	}
	return strings.Join(lines, "\n")
}

func renderSurveyOverlay(state feedbackSurveyState) string {
	lines := []string{
		"Feedback Survey",
		"",
	}
	switch state.Stage {
	case surveyThanks:
		lines = append(lines, "Thanks for the feedback.")
		if state.LastResponse > 0 {
			lines = append(lines, fmt.Sprintf("Latest rating: %d (%s)", state.LastResponse, surveyRatingLabel(state.LastResponse)))
		}
		lines = append(lines, "", "Press Esc to close.")
	default:
		lines = append(lines, "How helpful was the latest response?", "")
		lines = append(lines, "[1] unhelpful")
		lines = append(lines, "[2] weak")
		lines = append(lines, "[3] okay")
		lines = append(lines, "[4] good")
		lines = append(lines, "[5] excellent")
	}
	return strings.Join(lines, "\n")
}

func latestCompactMarker(session *state.Session) string {
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Hidden && strings.Contains(msg.Content, "compressed") {
			return summarizeCompactMessage(msg.Content)
		}
	}
	return ""
}

func visibleMessageCount(session *state.Session) int {
	count := 0
	for _, msg := range session.Messages {
		if !msg.Hidden {
			count++
		}
	}
	return count
}

func hiddenMessageCount(session *state.Session) int {
	count := 0
	for _, msg := range session.Messages {
		if msg.Hidden && msg.Role != "tool" {
			count++
		}
	}
	return count
}

func dedupeTasks(items []taskSnapshot) []taskSnapshot {
	out := make([]taskSnapshot, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		key := item.Name + ":" + item.Status + ":" + item.UpdatedAt.Format(time.RFC3339Nano)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func renderPermissionDialog(request permissions.Request) string {
	switch request.ToolName {
	case "bash":
		return renderBashPermissionDialog(request)
	case "file_read", "file_write", "file_edit":
		return renderFilePermissionDialog(request)
	case "web_fetch":
		return renderWebPermissionDialog(request)
	case "agent":
		return renderAgentPermissionDialog(request)
	case "team_create", "team_delete":
		return renderTeamPermissionDialog(request)
	default:
		if strings.HasPrefix(request.ToolName, "mcp__") {
			return renderMCPPermissionDialog(request)
		}
		lines := permissionDialogHeader(request, "Permission Request")
		lines = appendPermissionDetails(lines, request.Metadata)
		lines = append(lines, "", permissionDecisionKeys(request))
		return strings.Join(lines, "\n")
	}
}

func renderBashPermissionDialog(request permissions.Request) string {
	lines := permissionDialogHeader(request, "Bash Permission Request")
	if command := request.Metadata["command"]; command != "" {
		lines = append(lines, "Command:", command)
	}
	lines = appendPermissionDetails(lines, filterPermissionMetadata(request.Metadata, "command", "command_prefix"))
	if warning := bashWarningForCommand(request.Metadata["command"]); warning != "" {
		lines = append(lines, "", "Warning:", "- "+warning)
	}
	if label := shellSuggestionLabel(request); label != "" {
		lines = append(lines, "", "Persisted allow hint:", "- "+label)
	}
	lines = append(lines, "", "Decision:", "- allow command execution", "- deny and return to prompt", "", permissionDecisionKeys(request))
	return strings.Join(lines, "\n")
}

func renderFilePermissionDialog(request permissions.Request) string {
	title := "Filesystem Permission Request"
	if request.ToolName == "file_edit" {
		title = "File Edit Permission Request"
	}
	lines := permissionDialogHeader(request, title)
	if path := request.Metadata["path"]; path != "" {
		lines = append(lines, "Path: "+path)
	}
	if oldValue := request.Metadata["old"]; oldValue != "" {
		lines = append(lines, "Old: "+oldValue)
	}
	if newValue := request.Metadata["new"]; newValue != "" {
		lines = append(lines, "New: "+newValue)
	}
	lines = appendPermissionDetails(lines, filterPermissionMetadata(request.Metadata, "path", "filename", "old", "new"))
	lines = append(lines, "", permissionDecisionKeys(request))
	return strings.Join(lines, "\n")
}

func renderWebPermissionDialog(request permissions.Request) string {
	lines := permissionDialogHeader(request, "Web Fetch Permission Request")
	if url := request.Metadata["url"]; url != "" {
		lines = append(lines, "URL: "+url)
	}
	lines = appendPermissionDetails(lines, filterPermissionMetadata(request.Metadata, "url"))
	lines = append(lines, "", "Decision:", "- allow HTTP fetch", "- deny and return to prompt")
	lines = append(lines, "", permissionDecisionKeys(request))
	return strings.Join(lines, "\n")
}

func renderAgentPermissionDialog(request permissions.Request) string {
	lines := permissionDialogHeader(request, "Agent Permission Request")
	if description := request.Metadata["description"]; description != "" {
		lines = append(lines, "Task: "+description)
	}
	if teamName := request.Metadata["team_name"]; teamName != "" {
		lines = append(lines, "Team: "+teamName)
	}
	if subagentType := request.Metadata["subagent_type"]; subagentType != "" {
		lines = append(lines, "Subagent type: "+subagentType)
	}
	if model := request.Metadata["model"]; model != "" {
		lines = append(lines, "Model: "+model)
	}
	lines = appendPermissionDetails(lines, filterPermissionMetadata(request.Metadata, "description", "team_name", "subagent_type", "model"))
	lines = append(lines, "", "Decision:", "- run isolated subagent request", "- deny and return to prompt")
	lines = append(lines, "", permissionDecisionKeys(request))
	return strings.Join(lines, "\n")
}

func renderMCPPermissionDialog(request permissions.Request) string {
	lines := permissionDialogHeader(request, "MCP Permission Request")
	if server := request.Metadata["server"]; server != "" {
		lines = append(lines, "Server: "+server)
	}
	if tool := request.Metadata["mcp_tool"]; tool != "" {
		lines = append(lines, "Tool: "+tool)
	}
	if uri := request.Metadata["uri"]; uri != "" {
		lines = append(lines, "URI: "+uri)
	}
	lines = appendPermissionDetails(lines, filterPermissionMetadata(request.Metadata, "server", "mcp_tool", "uri"))
	lines = append(lines, "", "Decision:", "- allow MCP tool invocation", "- deny and return to prompt")
	lines = append(lines, "", permissionDecisionKeys(request))
	return strings.Join(lines, "\n")
}

func renderTeamPermissionDialog(request permissions.Request) string {
	lines := permissionDialogHeader(request, "Team Permission Request")
	if teamName := request.Metadata["team_name"]; teamName != "" {
		lines = append(lines, "Team: "+teamName)
	}
	if operation := request.Metadata["operation"]; operation != "" {
		lines = append(lines, "Operation: "+operation)
	}
	lines = appendPermissionDetails(lines, filterPermissionMetadata(request.Metadata, "team_name", "operation"))
	lines = append(lines, "", "Decision:", "- allow team state change", "- deny and return to prompt")
	lines = append(lines, "", permissionDecisionKeys(request))
	return strings.Join(lines, "\n")
}

func permissionDialogHeader(request permissions.Request, title string) []string {
	lines := []string{
		title,
		"",
		fmt.Sprintf("Tool: %s", request.ToolName),
		fmt.Sprintf("Level: %s", request.Level),
	}
	if request.Summary != "" {
		lines = append(lines, "Summary: "+request.Summary)
	}
	return lines
}

func permissionDecisionKeys(request permissions.Request) string {
	if len(request.Suggestions) > 0 {
		return "[y] allow once  [a] always allow  [n] deny"
	}
	return "[y] allow once  [n] deny"
}

func appendPermissionDetails(lines []string, metadata map[string]string) []string {
	if len(metadata) == 0 {
		return lines
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines = append(lines, "", "Details:")
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("- %s: %s", key, metadata[key]))
	}
	return lines
}

func bashWarningForCommand(command string) string {
	lower := strings.ToLower(command)
	switch {
	case strings.Contains(lower, "rm -rf"):
		return "Potentially destructive recursive deletion detected"
	case strings.Contains(lower, "git reset --hard"):
		return "Potentially destructive git reset detected"
	case strings.Contains(lower, "dd if="):
		return "Low-level disk write command detected"
	default:
		return ""
	}
}

func shellSuggestionLabel(request permissions.Request) string {
	prefix := request.Metadata["command_prefix"]
	if prefix == "" {
		command := strings.TrimSpace(request.Metadata["command"])
		fields := strings.Fields(command)
		if len(fields) > 0 {
			prefix = fields[0]
		}
	}
	if prefix == "" {
		return ""
	}
	return fmt.Sprintf("Don't ask again for %s commands in this workspace", prefix)
}

func filterPermissionMetadata(metadata map[string]string, exclude ...string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	blocked := map[string]bool{}
	for _, key := range exclude {
		blocked[key] = true
	}
	filtered := map[string]string{}
	for key, value := range metadata {
		if blocked[key] {
			continue
		}
		filtered[key] = value
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func fallbackValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func abbreviateIdentifier(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}
