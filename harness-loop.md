Loop and Harness engineering: 7 files, 5 steps. Every config inside
Most builders fight the loop. The loop is fine. The folder underneath isn't set up.
Open .claude/ in any working Claude Code project and you find roughly seven things doing the actual work: CLAUDE.md, settings.json, hooks/, agents/, skills/, .mcp.json, and a state file like MEMORY.md.
Most builders have opened one of those files. Maybe two. That is why their loops stall on the third iteration.
By the end of this article you will know what each file does, the five loop steps that ride on top, the three failure modes that kill most first attempts, and the single next file to add tonight.
No framework. No subscription. One walkthrough with exact paths and exact contents.
The harness is the floor. Pour it first.
Two layers, one setup
The harness is the .claude/ folder. It does not change between runs.
The loop is what runs inside it: a goal, an action, a verification step, a memory write, and a decision to keep going or stop.
The harness is the kitchen. The loop is the recipe.
Both fail without the other. A kitchen with no recipe is unused space. A recipe with no kitchen is wishful thinking.
Most builders treat the whole thing as one blob ("my agent setup") and miss that failures live in different layers.
Token blowups, prompt fatigue, dropped permissions: harness problems. Loops that never converge, verifications that pass garbage, scheduled runs that drift: loop problems.
Naming the layer fixes the diagnosis. You stop rewriting prompts when the real bug is a missing permission.
I thought building the loop first would teach me which harness files I needed. It was the other way around.
The harness sets what each iteration is allowed to do. Permissions decide whether the loop can write to disk. Subagents decide whether verification runs in a clean context.
Skills decide whether the loop can specialize. Hooks decide whether the loop even gets to fire on the trigger you wanted.
Without those decisions locked in, the loop guesses. When the loop guesses, it fabricates: invented files, invented commands, passing tests that pass nothing.
The harness stops the guessing. So the order is harness first, loop second, always.
The harness, file by file
CLAUDE.md
The first file Claude Code reads on every launch. Its contents become standing context for the entire session.
Put the project shape there: directory layout, language and framework, commands that actually work, conventions the agent must respect, and an explicit list of things it must not do.
Lives at repo root, not buried in docs. Minimal working shape:
# Project: my-app
Stack: Next.js 14, TypeScript, Postgres, Tailwind.
Layout: `app/` (routes), `lib/` (helpers), `db/migrations/`.

## Commands
- `pnpm dev` - local
- `pnpm test` - vitest
- `pnpm db:migrate` - apply migrations

## Never
- Edit `db/migrations/*` after merge.
- Add deps without justification in the PR body.
- Bypass `lib/auth/` to access user data.
The trap is bloat. The paper Less Context, Better Agents (arXiv 2606.10209) measured task completion dropping from 91.6% to 71% purely from oversized standing context.
Keep it under 300 lines. Prune it weekly. Every added paragraph is a tax on every future turn.
The canonical reference is centminmod/my-claude-code-setup, which ships three working CLAUDE.md shapes side by side.
settings.json
Where the tool allowlist, environment variables, and hook registrations live.
Two locations matter for daily work: .claude/settings.json at repo root for repo-scoped rules, and ~/.claude/settings.json for your personal defaults.
Scope hierarchy resolves managed > project > local > user, so project always overrides personal.
The first move that pays off in one afternoon is an allow array for read-only Bash and MCP calls:
{
  "permissions": {
    "allow": [
      "Bash(ls:*)",
      "Bash(git status:*)",
      "Bash(git diff:*)",
      "Bash(cat:*)",
      "Read(*)"
    ],
    "deny": [
      "Bash(rm -rf:*)",
      "Bash(git push --force:*)"
    ]
  }
}
The agent stops blocking on permission prompts for every ls, git status, cat. Destructive ops still gate.
Full key reference: Claude Code docs - Settings. Keep secrets in .claude/settings.local.json and gitignore it.
hooks
Deterministic scripts that fire on tool events: PreToolUse before a tool runs, PostToolUse after, Stop when the agent finishes a turn.
Registered inside settings.json with a matcher pattern and a shell command. Canonical first hook: a PostToolUse matching Edit|Write that pipes the file through prettier.
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {"type": "command", "command": "npx prettier --write \"$CLAUDE_FILE_PATH\""}
        ]
      }
    ]
  }
}
Every edit now exits in a known state. This is your policy floor.
Without hooks, every run is a vibe. Keep hooks silent on success, loud only on failure. Reference: Claude Code docs - Hooks.
subagents
Live under .claude/agents/ as markdown files with YAML frontmatter. Main agent invokes them through the Task tool. They run in a fresh context window.
Minimal verifier subagent:
---
name: verifier
description: Reviews a diff against the goal spec. Invoke after every code change.
model: haiku
tools: [Read, Grep, Bash]
---

You are a verifier. Read the goal spec in `PROMPT.md`. Read the diff.
Return a JSON verdict: {passes: bool, failures: [{line, reason}]}.
Do not propose fixes. Do not run code. Do not be polite.
The reviewer that lives inside the maker's context always agrees with itself. Pulling review into a fresh context closes the loudest failure mode.
Reference: wshobson/agents (37K stars) for 194 ready-made shapes. For an adversarial verifier with 11 named shortcut-checks (relaxed tests, swallowed errors, fake renames), pull moonrunnerkc/swarm-orchestrator.
skills
Live under .claude/skills/ as folders containing SKILL.md with YAML frontmatter.
Load progressively: at session start, only name and description enter context. Full body loads only when the agent decides the trigger matches.
---
name: db-migration-writer
description: Writes Postgres migration files for this repo. Use when the user
  asks to add/alter a table, column, index, or constraint.
when_to_use: schema change requested, new feature requires a new column,
  index missing on a hot query path
---

# Steps
1. Read `db/schema.sql` to confirm current state.
2. Write the migration to `db/migrations/NNN_<verb>_<noun>.sql`.
3. Include both up and down. Test with `pnpm db:migrate --dry`.
4. Never touch existing migration files.
This discipline keeps a fifty-skill library from costing fifty skills' worth of tokens on every prompt.
Canonical pattern: anthropics/skills (155K stars). Maximal pre-built kit: affaan-m/ECC (222K stars).
Three skills built when you hit the same task a third time beat fifty skills built speculatively from a tutorial.
MCP
Servers declared in .mcp.json at repo root. Model Context Protocol is the spec that lets the loop call out to live external tools.
Three rules: only servers your current work uses, prefer official ones for credentialed tools, never install five "just in case".
{
  "mcpServers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}
    },
    "context7": {
      "command": "npx",
      "args": ["-y", "@upstash/context7-mcp"]
    }
  }
}
Anthropic-maintained set: modelcontextprotocol/servers (87K stars). 
Code-host integration: github/github-mcp-server (31K stars).
Live library docs (kills stale-API problems): upstash/context7 (58K stars). 
Discovery index: punkpeye/awesome-mcp-servers (89K stars).
The first mistake is enabling a server with write scope before you have a hook that logs every call.
state and memory
The seventh piece, the one most people skip until the third project goes sideways.
Shape: a MEMORY.md index file at a known path, plus a vault directory for project canon.
~/.claude/memory/
  MEMORY.md            # index, links to topic files below
  user-prefs.md        # preferences, terse-vs-verbose, voice
  project-decisions.md # "we picked Postgres over Mongo on 2026-03-12, here is why"
  feedback-recent.md   # corrections you keep applying

~/vault/               # project canon (does not change session to session)
  architecture.md
  api-spec.md
  post-mortems/
Memory holds what changes across sessions. Vault holds what does not.
For production-grade session compression (200K-token transcript -> 4K-token recap without losing load-bearing facts): thedotmack/claude-mem (84K stars).
Theory behind why this matters: Anthropic engineering on context engineering names the failure mode: context rot.
The first mistake is treating memory as append-only. Prune it every session, or it becomes the rot.
The loop, on top of the harness
1. Goal spec
The external contract that says what "done" looks like. Lives on disk, not in the agent's head. The loop re-reads it every iteration.
Name: PROMPT.md, AGENTS.md, or AGENT_SPEC.md. The re-read is what matters.
# Goal
Migrate `users.password` from bcrypt to argon2id across the codebase.

# Done when
- All new password writes use argon2id (`lib/auth/hash.ts`).
- Existing bcrypt hashes are rehashed on next successful login.
- Test suite green: `pnpm test auth`.

# Never touch
- `db/migrations/*` already merged.
- Anything under `legacy/`.
- The session cookie format.

# Stop if
- More than 3 files outside `lib/auth/` need edits.
- A test that already passes starts failing.
Without this file the agent drifts after about three iterations. Smallest possible reference: ghuntley/how-to-ralph-wiggum (1.7K stars) - PROMPT.md plus an IMPLEMENTATION_PLAN.md state file the loop updates in place.
When the spec is missing, failure looks like progress. Code is written, tests pass, the goal it solved is not yours.
2. Plan to Act to Verify
The minimum viable loop is three steps. The agent plans against the goal spec, executes, then a separate verification pass checks the result before the next iteration is allowed to start.
Fresh context each iteration is the Ralph pattern. State lives on disk in the spec file plus a running log.
#!/usr/bin/env bash
# minimal loop runner: fresh context each turn, state on disk
set -euo pipefail

while true; do
  # plan + act in fresh context
  claude -p "Read PROMPT.md, IMPLEMENTATION_PLAN.md. Do the next step. Commit on green."

  # verify in fresh context (different subagent)
  if claude -p "/verify"; then
    echo "iter ok"
  else
    echo "verify failed, will retry"
  fi

  # exit when spec says done
  grep -q "^STATUS: done$" IMPLEMENTATION_PLAN.md && break
  sleep 5
done
Canonical patterns and CLI starters: cobusgreyling/loop-engineering (3K stars).
Production TypeScript reference with verifyCompletion: vercel-labs/ralph-loop-agent (805 stars).
Full installable Plan-to-Work-to-Review-to-Release cycle: Chachamaru127/claude-code-harness (2.9K stars).
Drop the verify step and confident garbage compounds. Every wrong output becomes the next iteration's input.
3. Sub-agent fan-out
When one goal branches into many independent sub-jobs (analyze 10 articles, fix 5 files, search 8 sources), the loop spawns parallel subagents. Orchestrator synthesizes.
One bloated context cannot do this. Ten small ones can.
# claude-agent-sdk-python style fan-out
from claude_agent_sdk import Agent, run_parallel

orchestrator = Agent.load(".claude/agents/orchestrator.md")
workers = [Agent.load(".claude/agents/researcher.md") for _ in range(8)]

results = run_parallel([
    w.run(source=src) for w, src in zip(workers, sources)
])

synthesis = orchestrator.run(inputs=results)
Anthropic engineering on multi-agent research measured +90.2% on their internal eval against a single-agent baseline.
Official SDK: anthropics/claude-agent-sdk-python (7.4K stars). Heaviest public fan-out kit (60+ agent types, 314 MCP tools): ruvnet/ruflo (61K stars).
Skip the fan-out and the orchestrator drowns. One context loaded with ten jobs' worth of source material is the exact shape that triggers context rot.
4. Scheduler and persistence
What triggers the loop when you are not in the chair. cron, launchctl, systemd, a queue runner.
The scheduler is deliberately dumber than the agent. If the scheduler tries to think (branch on state, decide whether to skip), it fails silently for days.
# crontab: run the loop every 30 min, log to disk
*/30 * * * * cd ~/my-loop && ./run.sh >> logs/$(date +\%Y-\%m-\%d).log 2>&1
Or as a launchd plist on macOS:
<key>StartCalendarInterval</key>
<dict>
  <key>Minute</key><integer>0</integer>
</dict>
<key>WorkingDirectory</key><string>/Users/me/my-loop</string>
<key>ProgramArguments</key>
<array><string>/bin/bash</string><string>run.sh</string></array>
Persistence is the other half. Every iteration must serialize what it did, what it tried, what is next. Otherwise the scheduler wakes up to an agent that forgot the goal.
Pattern for promoting ad-hoc sessions into scheduled runs: Kanevry/session-orchestrator.
5. Failure modes
Three failure modes kill almost every first attempt:
(a) Confident garbage. Verify step missing or weak. Wrong outputs pass and compound across iterations.
(b) Context rot. Single long context where the model degrades past a threshold (Anthropic's term). Accuracy collapses around 200K tokens of accumulated history.
(c) Ralph Wiggum loops. Same iteration repeats because state on disk did not capture progress. The agent re-plans the step it already finished.
The Less Context, Better Agents paper (arXiv 2606.10209) measured full-history at 71% task completion versus prune-and-summarize at 91.6%, on a fraction of the tokens.
before: single-context loop, 1.48M tokens, 71% completion, three hidden hallucinations per run
after:  prune-and-summarize loop with verifier subagent, 553K tokens, 91.6% completion, every figure traced
moonrunnerkc/swarm-orchestrator catalogs the 11 shortcuts agents take to fake done: relaxed tests, swallowed errors, fake renames, stub returns, comment-deletion-as-fix.
Memorize the names. You will recognize them in your own logs.

A complete minimal setup wires all seven harness files into a working loop. The shape of a project directory looks like this:
my-loop/
├── .claude/
│   ├── CLAUDE.md            # standing context for every session
│   ├── settings.json        # allow array + PostToolUse prettier hook
│   ├── agents/
│   │   └── verifier.md      # Haiku, reviews diffs in fresh context
│   └── skills/
│       └── db-migration-writer/
│           └── SKILL.md     # one skill, used three+ times
├── .mcp.json                # github MCP, context7 MCP
├── PROMPT.md                # goal spec (loop reads each iteration)
├── IMPLEMENTATION_PLAN.md   # state file (loop writes each iteration)
├── MEMORY.md                # cross-session preferences
├── run.sh                   # the loop runner (Plan -> Act -> Verify)
└── logs/                    # persistence, one file per cron tick
The wiring is one-directional. The harness defines the rules, the loop runs inside them, the state file connects iteration N to iteration N+1.
A single iteration walks the seven harness files and the five loop pieces in this order: cron fires run.sh, which calls claude -p. Claude Code reads CLAUDE.md and settings.json (harness 1, 2), applies the PostToolUse hook on every edit (harness 3), reads PROMPT.md and IMPLEMENTATION_PLAN.md (loop step 1), plans and acts (loop step 2), dispatches the verifier subagent in a fresh context (harness 4 + loop step 2 verify), writes the result back to IMPLEMENTATION_PLAN.md (loop step 3), updates MEMORY.md if a new preference was learned (harness 7), exits. Cron waits for the next tick (loop step 4).
If any of the seven harness files is missing, a specific loop step degrades. No CLAUDE.md and the planner re-derives the project shape every iteration. No verifier subagent and the verify step happens in the main context and always passes. No MEMORY.md and the same correction gets re-applied every Tuesday.
Build the seven harness files once. The loop runs forever.
What to do tonight
Open your .claude/ folder. Run:
ls -la .claude/
Count the files.
If you see nothing or only settings.json, start with CLAUDE.md. Keep it under 300 lines. Copy a shape from centminmod/my-claude-code-setup.
If you have CLAUDE.md and settings.json but no agents/, add a verifier subagent next. Pull review out of the main context. Shape: wshobson/agents.
If you have agents/ but no skills/, promote one frequent task to a skill. The prompt you have copy-pasted three times this week. Read three SKILL.md files from anthropics/skills before you write your first one.
If you have all seven harness files but no loop running, pick one repeating job, write its goal spec, and put a Plan-Act-Verify loop on top. Closest installable starting point: Chachamaru127/claude-code-harness.
After choosing, do one thing: open the matching repo in a new tab and clone it.
The harness is the floor. Without it, every loop runs over a hole.

## X 精华素材补充（XMCP 筛选，2026-06-29）

筛选口径：只保留和 loop engineering + harness 有直接工程含义的内容；过滤掉纯转链、卖服务、空泛喊口号、模型新闻借势和没有可落地机制的营销号内容。以下 5 条可作为下一版文章的观点补强。

### 1. 先设计 loop，再交给 Claude Code / Codex 运行

Source: Shann³, 2026-06-23  
URL: https://x.com/shannholmberg/status/2069517799266070624

短摘录：poorly designed loop burns tokens。

为什么值得收：这条不是在夸某个工具，而是在说 loop 本身需要先被设计。它和本文的 `PROMPT.md`、`IMPLEMENTATION_PLAN.md`、verifier subagent 三件套直接对应。

可提炼观点：不要把 Claude Code、Codex 当作“自动变聪明”的黑盒。先写循环契约：目标、边界、状态、退出条件、验证方式，再把这个 spec 交给 harness 执行。否则循环越长，token 花费越高，产物越像 slop。

可落地到 harness：
- `PROMPT.md` 写清楚 done criteria 和 never touch。
- `IMPLEMENTATION_PLAN.md` 记录当前 iteration、已完成、下一步。
- verifier subagent 必须读 spec 和 diff，而不是只看最终回答。

### 2. Loop engineering 是 agent harness 的一部分，不是单独新技术

Source: Karan, 2026-06-23  
URL: https://x.com/kmeanskaran/status/2069536174604173375

短摘录：Loop Engineering is part of Agent Harness。

为什么值得收：这条把概念边界讲清楚：prompt engineering 关注输入，context engineering 关注上下文，而 agent harness 关注整个可运行系统。loop engineering 只是 harness 里的运行时闭环。

可提炼观点：不要把 loop engineering 写成一个新 buzzword。更准确的表达是：loop 是 harness 的执行机制；harness 还包括工具、权限、记忆、状态、沙箱、观测、验证、成本控制。

可落地到 harness：
- 文章里可以把层级改成：Prompt -> Context -> Harness -> Loop。
- loop 章节前加一句：loop 不替代 harness，它运行在 harness 之上。
- 诊断问题时先判断是 harness 缺件，还是 loop 策略错误。

### 3. 固定 workflow 不等于 agent harness

Source: Alex Booker, 2026-06-24  
URL: https://x.com/bookercodes/status/2069712521041162279

短摘录：fixed workflow... lacks an adaptive loop。

为什么值得收：这条能防止把普通流水线包装成 agent。固定 A -> B -> C 的 workflow 是控制基础设施，但不等于 agent harness；缺少 reasoning-action-observation 的自适应循环，就只是自动化脚本。

可提炼观点：harness 和 workflow 的区别在“是否允许状态驱动下一步”。真正的 agent harness 至少要有观察、判断、工具执行、验证、状态写回、下一步选择。固定 DAG 可以是 harness 的底座，但不是完整 loop。

可落地到 harness：
- `run.sh` 不能只顺序执行命令，还要读取 `IMPLEMENTATION_PLAN.md` 决定下一步。
- verifier 失败后不能直接重跑同一步，必须把失败证据写回状态。
- scheduler 只负责唤醒；决策权留给 loop 和 state。

### 4. Inner loop / outer loop / meta loop 分层

Source: Nnenna, 2026-06-26  
URL: https://x.com/nnennahacks/status/2070554180251426903

短摘录：Inner-loop... Outer-loop... Meta-loop...

为什么值得收：这条提供了比“Plan -> Act -> Verify”更高一层的结构语言，适合补进本文的 failure diagnosis 部分。

可提炼观点：inner loop 只回答“这次代码是否够用”；outer loop 回答“这次变更是否安全进入组织的 merge/deploy/operate 流程”；meta loop 回答“失败是否被系统吸收，下一次是否更好”。很多 loop engineering 失败，是把三层混在一个上下文里。

可落地到 harness：
- Inner loop: 本地测试、lint、类型检查、单次 diff verifier。
- Outer loop: PR 检查、CI、部署前 smoke test、权限和审计。
- Meta loop: 失败分类、postmortem、memory 更新、skills/hooks 调整。

### 5. Verification loop 必须区分“真的失败”和“没有跑完”

Source: Dan Mercede, 2026-06-27  
URL: https://x.com/danmercede/status/2070764438970589570

短摘录：Abstention is not refutation。

为什么值得收：这是最实用的一条负面经验。验证循环因为 rate limit 崩了，却把所有结果记成 0-0 abstention，最后报告成“全部 refuted/inconclusive”。这正是 agent harness 里最危险的假阴性。

可提炼观点：fail-closed 不等于把所有未知都判失败。verification harness 必须有三态甚至四态：pass、fail、blocked、not-run。否则工具崩溃、限流、超时、权限错误都会被错误折叠成“验证失败”或“证据反驳”。

可落地到 harness：
- verifier 输出 schema 必须包含 `status: pass | fail | blocked | not_run`。
- blocked 必须携带 machine-readable reason，例如 `rate_limited`、`timeout`、`missing_permission`、`test_env_unavailable`。
- loop 只能在 `pass` 时推进，在 `fail` 时修复，在 `blocked/not_run` 时先修验证环境。
