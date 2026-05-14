---
name: fireworks-tech-graph
description: >-
  Use when the user wants to create any technical diagram - architecture, data
  flow, flowchart, sequence, agent/memory, or concept map. Trigger on: "画图"
  "帮我画" "生成图" "做个图" "架构图" "流程图" "可视化一下" "出图"
  "generate diagram" "draw diagram" "visualize" or any system/flow description
  the user wants illustrated.
user-invocable: true
argument-hint: "<diagram request>"
allowed-tools: ["Artifact", "Bash(python3 *)"]
shell: bash
metadata:
  product:
    version: "1.0.6"
    category: "Diagrams"
    icon: "GRAPH"
  agentapi:
    run_as_job: true
    produces_artifacts: true
  policy:
    allowed_tools: ["Artifact", "Bash(python3 *)"]
    network_allowlist:
      - "api.github.com"
      - "github.com"
    artifact_content_types:
      - "image/svg+xml"
    marketplace_summary: "Generate technical diagrams such as architecture, flowchart, sequence, UML, ER, timeline, and concept maps."
    input_schema:
      type: object
      properties:
        prompt:
          type: string
          description: "Diagram topic, structure, style, and output requirements."
---

# Fireworks Tech Graph

Create a production-quality technical diagram as one self-contained SVG artifact.

## AgentAPI Contract

This skill uses a script-first artifact path. The script converts the user's
request into template data, renders a self-contained SVG file, and prints the
relative artifact path. You then register that generated file with the
`Artifact` tool.

```!
python3 "${CLAUDE_SKILL_DIR}/scripts/generate-agentapi-diagram.py" <<'FIREWORKS_TECH_GRAPH_PROMPT'
$ARGUMENTS
FIREWORKS_TECH_GRAPH_PROMPT
```

Use the `Artifact` tool exactly once with the `artifact_file_path` printed
above. The value is intentionally relative to the current workspace; do not
rewrite it as an absolute path.

- `filename`: use the printed `filename`
- `content_type`: `image/svg+xml`
- `file_path`: use the printed `artifact_file_path`

If the shell output contains `skill_error:`, do not call the `Artifact` tool.
Reply in the user's language with the friendly error from `skill_error:` and,
when useful, ask for the missing system components or relationships. Do not
expose raw exceptions, stack traces, shell commands, workspace paths, artifact
IDs, object paths, or download paths.

The shell output may contain `skill_log: {...}` diagnostic lines. These are for
backend execution history only. Do not summarize, quote, or expose them to the
user.

After the `Artifact` tool succeeds, reply briefly in the user's language that
the diagram is ready in the Artifacts panel. Do not include the SVG source.
Do not claim PNG output unless a PNG artifact was actually created.

## Diagram Scope

Infer the diagram type from the request:

- Architecture: clients, gateway, services, data stores, queues, replicas, clusters.
- Data flow: data sources, transformations, storage, read/write paths.
- Sequence: participants, ordered messages, lifelines.
- Flowchart: steps, decisions, success/failure paths.
- Concept map: central concept, branches, relationships.
- Timeline: phases, milestones, dependencies.
- Class/UML/ER: entities, relationships, attributes.

If the user provides a public GitHub URL, the script may use public repository
metadata to infer major components. Do not claim full source-code review or
precise internals unless the user supplied the relevant content in the
conversation or as an attachment.

## SVG Requirements

- The script produces a full `<svg ...>` document with `xmlns="http://www.w3.org/2000/svg"`.
- Default viewBox: `0 0 1200 800`; increase height if the diagram needs more rows.
- Use plain SVG elements only: `rect`, `circle`, `path`, `line`, `polyline`, `text`, `g`, `defs`, `marker`.
- Include arrow markers in `<defs>` when arrows are used.
- Keep labels readable: use 13-22px font sizes, no negative letter spacing, no text overlapping component borders.
- Prefer a clean light theme with high contrast:
  - background `#f8fafc`
  - primary `#0f766e`
  - accent `#2563eb`
  - storage `#7c3aed`
  - warning/async `#d97706`
  - borders `#cbd5e1`
  - text `#0f172a`
- Group related components with lightly tinted containers.
- Label important arrows with short data/action names.
- Include a small legend only when it clarifies colors or line styles.

## Architecture Diagram Pattern

For architecture requests, prefer this layout:

1. Top: users/clients.
2. Upper middle: entry points such as API, proxy, load balancer, CLI, SDK.
3. Middle: core service/runtime components.
4. Lower middle: coordination, replication, clustering, workers.
5. Bottom: persistent or external storage.

Show high availability and scaling with replicas, shards, fan-out arrows, or dashed failover paths.

## Output Discipline

The final response must not include the SVG source. The SVG belongs in the generated artifact only.
