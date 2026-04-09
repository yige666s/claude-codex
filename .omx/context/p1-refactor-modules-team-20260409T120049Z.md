# P1 Refactoring Modules Team Context

## Task Statement

Read `/Users/ding/projectSrc/claude-code/claude-go/REFACTORING_MODULES.md` and implement five P1 sub-requirements in parallel using five executor lanes.

## Desired Outcome

- Land reviewable progress on all five P1 tracks.
- Keep each worker on a bounded, low-conflict slice.
- Prefer pure-Go gaps that do not require external APIs, CGO, UI ports, or platform-specific tmux/iTerm integrations.
- End with verifiable code and tests, not only analysis.

## Known Facts / Evidence

- The P1 section in `REFACTORING_MODULES.md` identifies five tracks:
  1. BashTool security layer
  2. Permissions system
  3. Swarm multi-agent system
  4. Bash parser utilities
  5. AgentTool completion
- Current repo state has partial drift from the document:
  - `internal/harness/agent/lifecycle.go` already contains `filterIncompleteToolCalls`.
  - `internal/harness/agent/load_agents_dir.go` already exists.
  - `internal/harness/agent/fork.go` exists but is not fully integrated at the tool surface.
- Existing Go touchpoints:
  - Bash tool: `internal/harness/tools/bash/{bash.go,security.go,readonly.go,path_validation.go}`
  - Permissions: `internal/harness/permissions/{permissions.go,types.go,rule_parser.go,shell_matching.go,dangerous_patterns.go,auto_mode.go}`
  - Swarm: `internal/harness/swarm/{types.go,constants.go,team_file.go,inprocess_backend.go,permission_sync.go}`
  - Agent core/tool: `internal/harness/agent/*`, `internal/harness/tools/agent/agent.go`

## Constraints

- No new dependencies.
- Avoid external LLM/API integrations for this pass.
- Avoid CGO/tree-sitter work in this pass.
- Avoid platform-only tmux/iTerm worker backend work in this pass.
- Keep diffs small and independently testable.
- The git top-level is `/Users/ding/projectSrc/claude-code`; there are unrelated changes outside `claude-go`.
- Workers must not revert unrelated user changes.

## Unknowns / Open Questions

- Which P1 sub-slices can be completed safely without queryengine/session/UI dependencies.
- Whether some missing TS parity items should be stubbed, partially integrated, or deferred with tests.
- Whether existing agent/swarm abstractions already expose enough hooks for tool-level wiring.

## Likely Codebase Touchpoints

- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/tools/bash/bash.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/tools/bash/security.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/tools/bash/readonly.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/tools/bash/path_validation.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/permissions/permissions.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/permissions/types.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/permissions/auto_mode.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/swarm/permission_sync.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/swarm/inprocess_backend.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/agent/fork.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/agent/lifecycle.go`
- `/Users/ding/projectSrc/claude-code/claude-go/internal/harness/tools/agent/agent.go`

## Proposed Lane Split

1. BashTool permission orchestration:
   - Wire command safety, readonly detection, path constraints, and permission results into the executable bash tool path.
   - Add focused tests.
2. Permissions pure-logic completion:
   - Implement non-API missing pieces such as shadowed-rule detection and mode-transition helpers.
   - Add tests.
3. Swarm permission bridge / runtime glue:
   - Improve leader-worker permission sync and bounded reconnection/runtime glue within existing in-process/file-backed primitives.
   - Add tests.
4. Bash parser utility expansion:
   - Add reusable heredoc/token/path parsing helpers that unlock safer command inspection without tree-sitter.
   - Add tests.
5. AgentTool surface completion:
   - Expand tool input schema and integrate existing agent/fork/loading utilities where feasible without queryengine/session resurrection work.
   - Add tests.
