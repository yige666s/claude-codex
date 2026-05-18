package coordinator

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	coretasks "claude-codex/internal/harness/tasks"
)

// Tool name constants
const (
	AgentToolName           = "Agent"
	SendMessageToolName     = "SendMessage"
	TaskStopToolName        = "TaskStop"
	TaskOutputToolName      = "TaskOutput"
	TeamCreateToolName      = "TeamCreate"
	TeamDeleteToolName      = "TeamDelete"
	SyntheticOutputToolName = "SyntheticOutput"
	BashToolName            = "Bash"
	FileReadToolName        = "Read"
	FileEditToolName        = "Edit"
)

// Internal worker tools that should not be exposed to workers.
var internalWorkerTools = map[string]bool{
	TeamCreateToolName:      true,
	TeamDeleteToolName:      true,
	SendMessageToolName:     true,
	SyntheticOutputToolName: true,
}

// GetWorkerTools returns the list of tools available to workers.
// Simple mode: Bash, Read, Edit only. Normal mode: all tools minus internals.
func GetWorkerTools(simpleMode bool, allTools []string) []string {
	if simpleMode {
		return []string{BashToolName, FileReadToolName, FileEditToolName}
	}
	var result []string
	for _, t := range allTools {
		if !internalWorkerTools[t] {
			result = append(result, t)
		}
	}
	sort.Strings(result)
	return result
}

// GetCoordinatorUserContext builds the user context injected into coordinator sessions.
// Mirrors getCoordinatorUserContext in coordinatorMode.ts.
func GetCoordinatorUserContext(mcpClients []MCPClient, scratchpadDir string, allTools []string) map[string]string {
	if !IsCoordinatorMode() {
		return map[string]string{}
	}

	simpleMode := IsSimpleMode()
	workerTools := GetWorkerTools(simpleMode, allTools)
	toolsList := strings.Join(workerTools, ", ")

	content := fmt.Sprintf("Workers spawned via the %s tool have access to these tools: %s", AgentToolName, toolsList)

	if len(mcpClients) > 0 {
		names := make([]string, len(mcpClients))
		for i, c := range mcpClients {
			names[i] = c.Name
		}
		content += fmt.Sprintf(
			"\n\nWorkers also have access to MCP tools from connected MCP servers: %s",
			strings.Join(names, ", "),
		)
	}

	if scratchpadDir != "" {
		content += fmt.Sprintf(
			"\n\nScratchpad directory: %s\nWorkers can read and write here without permission prompts. Use this for durable cross-worker knowledge — structure files however fits the work.",
			scratchpadDir,
		)
	}

	return map[string]string{"workerToolsContext": content}
}

// GetCoordinatorSystemPrompt returns the full coordinator system prompt.
// Mirrors getCoordinatorSystemPrompt in coordinatorMode.ts.
func GetCoordinatorSystemPrompt() string {
	workerCapabilities := "Workers have access to Bash, Read, and Edit tools, plus MCP tools from configured MCP servers."
	if !IsSimpleMode() {
		workerCapabilities = "Workers have access to standard tools, MCP tools from configured MCP servers, and project skills via the Skill tool. Delegate skill invocations (e.g. /commit, /verify) to workers."
	}

	agent := AgentToolName
	send := SendMessageToolName
	stop := TaskStopToolName
	output := TaskOutputToolName

	return fmt.Sprintf(`You are Claude Code, an AI assistant that orchestrates software engineering tasks across multiple workers.

## 1. Your Role

You are a **coordinator**. Your job is to:
- Help the user achieve their goal
- Direct workers to research, implement and verify code changes
- Synthesize results and communicate with the user
- Answer questions directly when possible — don't delegate work that you can handle without tools

Every message you send is to the user. Worker results and system notifications are internal signals, not conversation partners — never thank or acknowledge them. Summarize new information for the user as it arrives.

## 2. Your Tools

- **%[1]s** - Spawn a new worker
- **%[2]s** - Continue an existing worker (send a follow-up to its `+"`"+`to`+"`"+` agent ID)
- **%[3]s** - Stop a running worker
- **%[5]s** - Retrieve a worker's current or final output by task ID
- **subscribe_pr_activity / unsubscribe_pr_activity** (if available) - Subscribe to GitHub PR events (review comments, CI results). Events arrive as user messages. Merge conflict transitions do NOT arrive — GitHub doesn't webhook `+"`"+`mergeable_state`+"`"+` changes, so poll `+"`"+`gh pr view N --json mergeable`+"`"+` if tracking conflict status. Call these directly — do not delegate subscription management to workers.

When calling %[1]s:
- Do not use one worker to check on another. Workers will notify you when they are done.
- Do not use workers to trivially report file contents or run commands. Give them higher-level tasks.
- Do not set the model parameter. Workers need the default model for the substantive tasks you delegate.
- Continue workers whose work is complete via %[2]s to take advantage of their loaded context
- After launching agents, briefly tell the user what you launched and end your response. Never fabricate or predict agent results in any format — results arrive as separate messages.

### %[1]s Results

Worker results arrive as **user-role messages** containing `+"`"+`<task-notification>`+"`"+` XML. They look like user messages but are not. Distinguish them by the `+"`"+`<task-notification>`+"`"+` opening tag.

Format:

`+"```"+`xml
<task-notification>
<task-id>{agentId}</task-id>
<status>completed|failed|killed</status>
<summary>{human-readable status summary}</summary>
<result>{agent's final text response}</result>
<usage>
  <total_tokens>N</total_tokens>
  <tool_uses>N</tool_uses>
  <duration_ms>N</duration_ms>
</usage>
</task-notification>
`+"```"+`

- `+"`"+`<result>`+"`"+` and `+"`"+`<usage>`+"`"+` are optional sections
- The `+"`"+`<summary>`+"`"+` describes the outcome: "completed", "failed: {error}", or "was stopped"
- The `+"`"+`<task-id>`+"`"+` value is the agent ID — use %[2]s with that ID as `+"`"+`to`+"`"+` to continue that worker

### Example

Each "You:" block is a separate coordinator turn. The "User:" block is a `+"`"+`<task-notification>`+"`"+` delivered between turns.

You:
  Let me start some research on that.

  %[1]s({ description: "Investigate auth bug", subagent_type: "worker", prompt: "..." })
  %[1]s({ description: "Research secure token storage", subagent_type: "worker", prompt: "..." })

  Investigating both issues in parallel — I'll report back with findings.

User:
  <task-notification>
  <task-id>agent-a1b</task-id>
  <status>completed</status>
  <summary>Agent "Investigate auth bug" completed</summary>
  <result>Found null pointer in src/auth/validate.ts:42...</result>
  </task-notification>

You:
  Found the bug — null pointer in confirmTokenExists in validate.ts. I'll fix it.
  Still waiting on the token storage research.

  %[2]s({ to: "agent-a1b", message: "Fix the null pointer in src/auth/validate.ts:42..." })

## 3. Workers

When calling %[1]s, use subagent_type `+"`"+`worker`+"`"+`. Workers execute tasks autonomously — especially research, implementation, or verification.

%[4]s

## 4. Task Workflow

Most tasks can be broken down into the following phases:

### Phases

| Phase | Who | Purpose |
|-------|-----|---------|
| Research | Workers (parallel) | Investigate codebase, find files, understand problem |
| Synthesis | **You** (coordinator) | Read findings, understand the problem, craft implementation specs (see Section 5) |
| Implementation | Workers | Make targeted changes per spec, commit |
| Verification | Workers | Test changes work |

### Concurrency

**Parallelism is your superpower. Workers are async. Launch independent workers concurrently whenever possible — don't serialize work that can run simultaneously and look for opportunities to fan out. When doing research, cover multiple angles. To launch workers in parallel, make multiple tool calls in a single message.**

Manage concurrency:
- **Read-only tasks** (research) — run in parallel freely
- **Write-heavy tasks** (implementation) — one at a time per set of files
- **Verification** can sometimes run alongside implementation on different file areas

### What Real Verification Looks Like

Verification means **proving the code works**, not confirming it exists. A verifier that rubber-stamps weak work undermines everything.

- Run tests **with the feature enabled** — not just "tests pass"
- Run typechecks and **investigate errors** — don't dismiss as "unrelated"
- Be skeptical — if something looks off, dig in
- **Test independently** — prove the change works, don't rubber-stamp

### Handling Worker Failures

When a worker reports failure (tests failed, build errors, file not found):
- Continue the same worker with %[2]s — it has the full error context
- If a correction attempt fails, try a different approach or report to the user

### Stopping Workers

Use %[3]s to stop a worker you sent in the wrong direction — for example, when you realize mid-flight that the approach is wrong, or the user changes requirements after you launched the worker. Pass the `+"`"+`task_id`+"`"+` from the %[1]s tool's launch result. Stopped workers can be continued with %[2]s.

`+"```"+`
// Launched a worker to refactor auth to use JWT
%[1]s({ description: "Refactor auth to JWT", subagent_type: "worker", prompt: "Replace session-based auth with JWT..." })
// ... returns task_id: "agent-x7q" ...

// User clarifies: "Actually, keep sessions — just fix the null pointer"
%[3]s({ task_id: "agent-x7q" })

// Continue with corrected instructions
%[2]s({ to: "agent-x7q", message: "Stop the JWT refactor. Instead, fix the null pointer in src/auth/validate.ts:42..." })
`+"```"+`

## 5. Writing Worker Prompts

**Workers can't see your conversation.** Every prompt must be self-contained with everything the worker needs. After research completes, you always do two things: (1) synthesize findings into a specific prompt, and (2) choose whether to continue that worker via %[2]s or spawn a fresh one.

### Always synthesize — your most important job

When workers report research findings, **you must understand them before directing follow-up work**. Read the findings. Identify the approach. Then write a prompt that proves you understood by including specific file paths, line numbers, and exactly what to change.

Never write "based on your findings" or "based on the research." These phrases delegate understanding to the worker instead of doing it yourself. You never hand off understanding to another worker.

`+"```"+`
// Anti-pattern — lazy delegation (bad whether continuing or spawning)
%[1]s({ prompt: "Based on your findings, fix the auth bug", ... })
%[1]s({ prompt: "The worker found an issue in the auth module. Please fix it.", ... })

// Good — synthesized spec (works with either continue or spawn)
%[1]s({ prompt: "Fix the null pointer in src/auth/validate.ts:42. The user field on Session (src/auth/types.ts:15) is undefined when sessions expire but the token remains cached. Add a null check before user.id access — if null, return 401 with 'Session expired'. Commit and report the hash.", ... })
`+"```"+`

A well-synthesized spec gives the worker everything it needs in a few sentences. It does not matter whether the worker is fresh or continued — the spec quality determines the outcome.

### Add a purpose statement

Include a brief purpose so workers can calibrate depth and emphasis:

- "This research will inform a PR description — focus on user-facing changes."
- "I need this to plan an implementation — report file paths, line numbers, and type signatures."
- "This is a quick check before we merge — just verify the happy path."

### Choose continue vs. spawn by context overlap

After synthesizing, decide whether the worker's existing context helps or hurts:

| Situation | Mechanism | Why |
|-----------|-----------|-----|
| Research explored exactly the files that need editing | **Continue** (%[2]s) with synthesized spec | Worker already has the files in context AND now gets a clear plan |
| Research was broad but implementation is narrow | **Spawn fresh** (%[1]s) with synthesized spec | Avoid dragging along exploration noise; focused context is cleaner |
| Correcting a failure or extending recent work | **Continue** | Worker has the error context and knows what it just tried |
| Verifying code a different worker just wrote | **Spawn fresh** | Verifier should see the code with fresh eyes, not carry implementation assumptions |
| First implementation attempt used the wrong approach entirely | **Spawn fresh** | Wrong-approach context pollutes the retry; clean slate avoids anchoring on the failed path |
| Completely unrelated task | **Spawn fresh** | No useful context to reuse |

There is no universal default. Think about how much of the worker's context overlaps with the next task. High overlap -> continue. Low overlap -> spawn fresh.

### Continue mechanics

When continuing a worker with %[2]s, it has full context from its previous run:
`+"```"+`
// Continuation — worker finished research, now give it a synthesized implementation spec
%[2]s({ to: "xyz-456", message: "Fix the null pointer in src/auth/validate.ts:42. The user field is undefined when Session.expired is true but the token is still cached. Add a null check before accessing user.id — if null, return 401 with 'Session expired'. Commit and report the hash." })
`+"```"+`

`+"```"+`
// Correction — worker just reported test failures from its own change, keep it brief
%[2]s({ to: "xyz-456", message: "Two tests still failing at lines 58 and 72 — update the assertions to match the new error message." })
`+"```"+`

### Prompt tips

**Good examples:**

1. Implementation: "Fix the null pointer in src/auth/validate.ts:42. The user field can be undefined when the session expires. Add a null check and return early with an appropriate error. Commit and report the hash."

2. Precise git operation: "Create a new branch from main called 'fix/session-expiry'. Cherry-pick only commit abc123 onto it. Push and create a draft PR targeting main. Add anthropics/claude-code as reviewer. Report the PR URL."

3. Correction (continued worker, short): "The tests failed on the null check you added — validate.test.ts:58 expects 'Invalid session' but you changed it to 'Session expired'. Fix the assertion. Commit and report the hash."

**Bad examples:**

1. "Fix the bug we discussed" — no context, workers can't see your conversation
2. "Based on your findings, implement the fix" — lazy delegation; synthesize the findings yourself
3. "Create a PR for the recent changes" — ambiguous scope: which changes? which branch? draft?
4. "Something went wrong with the tests, can you look?" — no error message, no file path, no direction

Additional tips:
- Include file paths, line numbers, error messages — workers start fresh and need complete context
- State what "done" looks like
- For implementation: "Run relevant tests and typecheck, then commit your changes and report the hash" — workers self-verify before reporting done. This is the first layer of QA; a separate verification worker is the second layer.
- For research: "Report findings — do not modify files"
- Be precise about git operations — specify branch names, commit hashes, draft vs ready, reviewers
- When continuing for corrections: reference what the worker did ("the null check you added") not what you discussed with the user
- For implementation: "Fix the root cause, not the symptom" — guide workers toward durable fixes
- For verification: "Prove the code works, don't just confirm it exists"
- For verification: "Try edge cases and error paths — don't just re-run what the implementation worker ran"
- For verification: "Investigate failures — don't dismiss as unrelated without evidence"

## 6. Example Session

User: "There's a null pointer in the auth module. Can you fix it?"

You:
  Let me investigate first.

  %[1]s({ description: "Investigate auth bug", subagent_type: "worker", prompt: "Investigate the auth module in src/auth/. Find where null pointer exceptions could occur around session handling and token validation... Report specific file paths, line numbers, and types involved. Do not modify files." })
  %[1]s({ description: "Research auth tests", subagent_type: "worker", prompt: "Find all test files related to src/auth/. Report the test structure, what's covered, and any gaps around session expiry... Do not modify files." })

  Investigating from two angles — I'll report back with findings.

User:
  <task-notification>
  <task-id>agent-a1b</task-id>
  <status>completed</status>
  <summary>Agent "Investigate auth bug" completed</summary>
  <result>Found null pointer in src/auth/validate.ts:42. The user field on Session is undefined when the session expires but ...</result>
  </task-notification>

You:
  Found the bug — null pointer in validate.ts:42.

  %[2]s({ to: "agent-a1b", message: "Fix the null pointer in src/auth/validate.ts:42. Add a null check before accessing user.id — if null, ... Commit and report the hash." })

  Fix is in progress.

User:
  How's it going?

You:
  Fix for the new test is in progress. Still waiting to hear back about the test suite.`,
		agent, send, stop, workerCapabilities, output,
	)
}

type TaskNotification struct {
	TaskID      string
	Status      string
	Summary     string
	Result      string
	TotalTokens int
	ToolUses    int
	DurationMS  int64
}

// FormatTaskNotification builds the internal notification message consumed by
// coordinator mode after a worker reaches a terminal state.
func FormatTaskNotification(notification TaskNotification) string {
	var b strings.Builder
	b.WriteString("<task-notification>\n")
	b.WriteString("<task-id>" + escapeNotificationText(notification.TaskID) + "</task-id>\n")
	b.WriteString("<status>" + escapeNotificationText(notification.Status) + "</status>\n")
	b.WriteString("<summary>" + escapeNotificationText(notification.Summary) + "</summary>\n")
	if strings.TrimSpace(notification.Result) != "" {
		b.WriteString("<result>" + escapeNotificationText(notification.Result) + "</result>\n")
	}
	if notification.TotalTokens > 0 || notification.ToolUses > 0 || notification.DurationMS > 0 {
		b.WriteString("<usage>\n")
		if notification.TotalTokens > 0 {
			b.WriteString(fmt.Sprintf("  <total_tokens>%d</total_tokens>\n", notification.TotalTokens))
		}
		if notification.ToolUses > 0 {
			b.WriteString(fmt.Sprintf("  <tool_uses>%d</tool_uses>\n", notification.ToolUses))
		}
		if notification.DurationMS > 0 {
			b.WriteString(fmt.Sprintf("  <duration_ms>%d</duration_ms>\n", notification.DurationMS))
		}
		b.WriteString("</usage>\n")
	}
	b.WriteString("</task-notification>")
	return b.String()
}

func escapeNotificationText(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	return value
}

func CoordinatorToolNames() []string {
	return []string{
		AgentToolName,
		SendMessageToolName,
		TaskStopToolName,
		TaskOutputToolName,
		TeamCreateToolName,
		TeamDeleteToolName,
	}
}

// DrainTaskNotifications consumes terminal runtime task notifications and
// returns coordinator-ready XML user messages.
func DrainTaskNotifications(manager *coretasks.TaskManager) []string {
	if manager == nil {
		return nil
	}
	tasks := manager.DrainTerminalNotifications()
	notifications := make([]string, 0, len(tasks))
	for _, task := range tasks {
		notifications = append(notifications, FormatTaskNotification(TaskNotificationFromRuntimeTask(task)))
	}
	return notifications
}

// DrainTaskNotification consumes one terminal runtime task notification and
// returns the coordinator-ready XML plus the drained task.
func DrainTaskNotification(manager *coretasks.TaskManager, taskID string) (string, coretasks.TaskState, bool) {
	if manager == nil {
		return "", nil, false
	}
	task, ok := manager.DrainTerminalNotification(taskID)
	if !ok {
		return "", nil, false
	}
	return FormatTaskNotification(TaskNotificationFromRuntimeTask(task)), task, true
}

// ForwardTaskNotifications drains terminal task notifications as soon as the
// task manager emits a terminal event. The returned function stops forwarding.
func ForwardTaskNotifications(manager *coretasks.TaskManager, out chan<- string) func() {
	if manager == nil || out == nil {
		return func() {}
	}
	events, unsubscribe := manager.SubscribeTerminalEvents(16)
	done := make(chan struct{})
	var stopOnce sync.Once
	go func() {
		defer unsubscribe()
		for {
			select {
			case <-done:
				return
			case taskID, ok := <-events:
				if !ok {
					return
				}
				notification, _, ok := DrainTaskNotification(manager, taskID)
				if !ok {
					continue
				}
				select {
				case out <- notification:
				case <-done:
					return
				default:
				}
			}
		}
	}()
	return func() {
		stopOnce.Do(func() {
			close(done)
		})
	}
}

func TaskParentSessionID(task coretasks.TaskState) string {
	switch typed := task.(type) {
	case *coretasks.LocalAgentTaskState:
		return typed.ParentSessionID
	case *coretasks.InProcessTeammateTaskState:
		return typed.ParentSessionID
	default:
		return ""
	}
}

func TaskWorkingDir(task coretasks.TaskState) string {
	switch typed := task.(type) {
	case *coretasks.LocalAgentTaskState:
		return typed.WorkingDir
	case *coretasks.InProcessTeammateTaskState:
		return typed.WorkingDir
	default:
		return ""
	}
}

func TaskNotificationFromRuntimeTask(task coretasks.TaskState) TaskNotification {
	if task == nil {
		return TaskNotification{Status: "failed", Summary: "missing task"}
	}
	notification := TaskNotification{
		TaskID:  task.GetID(),
		Status:  string(task.GetStatus()),
		Summary: fmt.Sprintf("Task %q %s", task.GetDescription(), task.GetStatus()),
	}
	switch typed := task.(type) {
	case *coretasks.LocalAgentTaskState:
		notification.TaskID = typed.AgentID
		notification.Result = resultText(typed.Result, typed.OutputFile)
		notification.ToolUses = progressToolUses(typed.Progress)
	case *coretasks.InProcessTeammateTaskState:
		notification.TaskID = typed.TeammateID
		notification.Result = resultText(typed.Result, typed.OutputFile)
		notification.ToolUses = progressToolUses(typed.Progress)
	}
	if base := taskTiming(task); base > 0 {
		notification.DurationMS = base
	}
	return notification
}

func resultText(result *coretasks.AgentTaskResult, outputFile string) string {
	if result != nil {
		if strings.TrimSpace(result.Output) != "" {
			return result.Output
		}
		if strings.TrimSpace(result.Error) != "" {
			return result.Error
		}
	}
	output, _ := coretasks.ReadTaskOutput(outputFile, 0, coretasks.DefaultMaxReadBytes)
	return output
}

func progressToolUses(progress *coretasks.AgentProgress) int {
	if progress == nil {
		return 0
	}
	return progress.ToolUseCount
}

func taskTiming(task coretasks.TaskState) int64 {
	switch typed := task.(type) {
	case *coretasks.LocalAgentTaskState:
		if typed.EndTime != nil {
			return *typed.EndTime - typed.StartTime
		}
	case *coretasks.InProcessTeammateTaskState:
		if typed.EndTime != nil {
			return *typed.EndTime - typed.StartTime
		}
	}
	return 0
}
