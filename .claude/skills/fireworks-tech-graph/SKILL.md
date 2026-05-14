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
allowed-tools: ["Artifact"]
metadata:
  product:
    version: "1.0.5"
    category: "Diagrams"
    icon: "GRAPH"
  agentapi:
    run_as_job: true
    produces_artifacts: true
  policy:
    allowed_tools: ["Artifact"]
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

You have exactly one user-facing output path in this environment: the `Artifact` tool.

Before any final answer:

1. Convert the user's request into a concise diagram plan.
2. Generate a complete, valid SVG document as text.
3. Call `Artifact` exactly once:
   - `filename`: short kebab-case name ending in `.svg`
   - `content_type`: `image/svg+xml`
   - `content`: the complete SVG text
4. After `Artifact` succeeds, reply briefly in the user's language that the diagram is ready in the Artifacts panel.

Do not claim that work is in progress. Do not report local file paths. Do not mention internal tools, workspace paths, shell commands, package names, or validation scripts. Do not claim PNG output unless a PNG artifact was actually created.

## Diagram Scope

Infer the diagram type from the request:

- Architecture: clients, gateway, services, data stores, queues, replicas, clusters.
- Data flow: data sources, transformations, storage, read/write paths.
- Sequence: participants, ordered messages, lifelines.
- Flowchart: steps, decisions, success/failure paths.
- Concept map: central concept, branches, relationships.
- Timeline: phases, milestones, dependencies.
- Class/UML/ER: entities, relationships, attributes.

If the user provides a URL or repository name, use generally known public product/domain knowledge only. Do not say you inspected the repository unless the user supplied source content in the conversation or as an attachment.

## SVG Requirements

- Produce a full `<svg ...>` document with `xmlns="http://www.w3.org/2000/svg"`.
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

The final response must not include the SVG source. The SVG belongs in the `Artifact` tool call only.
