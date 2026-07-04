---
name: Presentations
description: Create or edit PowerPoint or Google Slides decks
user-invocable: true
allowed-tools: ["Artifact", "Bash"]
shell: bash
metadata:
  product:
    version: "26.623.12021"
    category: "Office"
    icon: "PPTX"
  agentapi:
    run_as_job: true
    long_running: true
    produces_artifacts: true
    policy:
      artifact_content_types:
        - application/vnd.openxmlformats-officedocument.presentationml.presentation
        - application/pdf
        - image/png
        - image/webp
      allowed_tools:
        - Artifact
        - Bash
      allowed_env:
        - HOME
        - TMPDIR
        - AGENT_WORKSPACE_DIR
        - CLAUDE_SKILL_DIR
        - NODE_PATH
        - OFFICE_RUNTIME_NODE_MODULES
      sandbox:
        runner: local
        network: none
---

# Slides Skill

## AgentAPI Runtime Adaptation

This export references Codex's private `@oai/artifact-tool` package. That
package is not bundled with this repository and is not available from the
public npm registry. In this AgentAPI project, use the provided public runtime
instead:

- Preferred local PPTX authoring: `python3` with `python-pptx`
- JS authoring option: `node` with global `pptxgenjs`
- Visual checks: LibreOffice (`soffice`) plus Poppler (`pdftoppm`, `pdfinfo`)
- Runtime smoke check:
  `python3 "$CLAUDE_SKILL_DIR/../../skill-runtimes/office/check_office_runtime.py"`
- Node package path: `${OFFICE_RUNTIME_NODE_MODULES:-/usr/local/lib/node_modules}`

When the reference material below says to use `@oai/artifact-tool`, adapt the
implementation to `python-pptx` or `pptxgenjs` while preserving the same deck
quality gates: complete content, no overlaps, readable typography, rendered
preview inspection, and final PPTX artifact registration.

Use this skill as reference material when creating or editing presentation slide decks.

## Important Instructions

- [HARD REQUIREMENT] Content: ensure the deck covers everything the user requested.
- Storytelling: ensure the overall deck has a coherent story and a logical slide-to-slide flow.
- Info density: avoid cramming low-value details onto a single slide. Prefer lower-density slides with high-value content.
  - Title slide: keep the title slide minimal and simple. Avoid cramming in too much information.
- Layout: keep things clean and simple. Avoid low-quality visuals, but also avoid excessive white space. By default, use equal left and right margins on each slide.
- [HARD REQUIREMENT] Overlap: always pay attention to programmatic overlap warnings. Do not assume that overlapping elements in diagrams are intentional, and do not ignore overlap warnings without inspecting them. You MUST fix all unintended overlap errors before delivering the slides. This is critical.
- [HARD REQUIREMENT] Font size: when a template is provided, match its font sizes. When no template or style guidance is given, you MUST use at least 50pt for deck titles, 35pt for slide titles, 24pt for mid-level text such as subheadings, callout headers, and text-box titles, and 16pt for body text.
- Text layout: when there is too much text, shorten it before shrinking the font size. Inspect visually for unexpected text wrapping. NEVER allow a title/banner text box intended for one line to wrap to two lines.
- Visual assets:
  - [HARD REQUIREMENT] DO NOT use Python to draw images; DO NOT use programmatic vector shapes for visuals; DO NOT use programmatic drawings of any sort. Use image search or image_gen tool instead!
  - [HARD REQUIREMENT] Minimize the use of diagrams. Add them only when requested or when a single diagram materially improves the clarity of complex concepts. Diagram implementation rules: use native PowerPoint shapes for simple diagrams; use Graphviz for complex relational/topological/network-like diagrams; use image_gen for highly aesthetic, illustrative, or scientific infographic diagrams (e.g. chemical structures, circuit diagrams, etc.). When using native PowerPoint shapes with connectors, create connectors (arrows/edges) before creating entity nodes, so edges appear behind nodes and never cross through node shapes or labels. If this ordering is awkward during early iteration, you may create nodes first in the initial draft, then switch to connectors-first in the revised code.
  - Before sourcing or generating visuals, be mindful of the desired aspect ratio, placement, and cropping options on the slide. For example, if you intend to place text to the left of the image containing a person, you should ask image_gen to put the person on the right side of the image.
  - By default, DO NOT reuse the same image more than once (unless it's a background).
  - Prepare visuals for both the main concept and decorative support.
- Default styling: use one composition instead of a collection of UI panels. UI-like styling typically includes card grids, pills, badges, button-like text boxes, tab or navigation patterns, repeated modular panels, dense dashboard-style layouts, and other component-library aesthetics that imply interactivity. Use stylized text boxes sparingly, favoring a flat structure on the canvas.

## Skill Folder Contents

Contents of the `slides/` skill folder:

- `container_tools/`: Standalone python scripts for slides and relevant asset manipulation.
- `references/`: Additional workflow references for specialized presentation tasks.
- `template_following_scripts/`: Helper scripts for exact source-deck/template following.
- `artifact_tool/`: API documentation and coding examples for the artifact tool library.
- `assets/artifact-md/codex-grid-layout-library/`: A private, source-free Artifact.md package with 80 rendered layout previews, a model-facing registry, structured content tokens, and 80 exact plain-JavaScript artifact-tool Compose reconstructions with no JSX.

## AgentAPI Fast Path

For AgentAPI PPTX jobs, default to the fast local authoring path unless the user
explicitly provides a template/reference deck or asks for a custom visual system.
This keeps durable skill jobs inside their time budget.

For a normal request such as "generate a PPT from this document":

1. Do not read the Codex Grid layout library, design tokens, template registry,
   layout previews, or artifact-tool implementation modules.
2. Do not inspect template-following scripts or private artifact-tool docs.
3. Use the provided `$SKILL_DIR`, `$CLAUDE_SKILL_DIR`, `$TMP_DIR`, and `$TMPDIR`
   environment variables. Do not overwrite them with `$PWD` or the workspace.
4. Create a concise outline directly from the user content, then generate a real
   PPTX with `python-pptx` in one script.
5. Use a simple professional business theme: title slide, agenda, 6-10 content
   slides, and closing slide unless the user requests a different count.
6. Register only the final `.pptx` with Artifact using `file_path`.

## Codex Grid Artifact-Tool Compose Layout Reference

This skill variant includes a distilled layout library, but AgentAPI must treat
it as an optional design reference, not the default execution path. Use Codex
Grid only when the user explicitly asks for advanced layout exploration, a
specific visual system, or a deck style that cannot be satisfied by the fast
local `python-pptx` route.

When Codex Grid is explicitly needed, before planning slides:

1. Read `assets/artifact-md/codex-grid-layout-library/ARTIFACT.md`, `design_tokens.json`, and `artifact-tool-compose/template-registry.json`.
2. Inspect `assets/previews/layout-library.png`, then shortlist layouts by `templateUse`, `layoutFamily`, `slots`, `densityBudget`, and `typographyBudget`. Do not open all 80 implementation modules by default.
3. For each selected layout, inspect its generated preview and exact `artifact-tool-compose/slide-XX.mjs` reconstruction.
4. Use the selected module's `layers(...)`, `text(...)`, `shape(...)`, `image(...)`, and `table(...)` helper calls as the implementation reference. Keep the output as plain `.mjs` and use `slide.compose(...)`; do not introduce JSX or a transpilation step.
5. Preserve the selected layout's content ownership, spacing, hierarchy, and media frames while replacing instructional sample text with the user's content. Vary silhouettes across the deck instead of repeating one pattern.

The package's `scripts/snippets/create-presentation.mjs` materializes the complete library for validation; it is not a request to emit all 80 layouts in the user's deck. User-provided templates, explicit brand guidance, and exact source evidence always override this default library.

## Workspace

Use the chat mode supplied by Codex. If the chat is not projectless, use the
project-backed layout.

Set:

- `SKILL_DIR=<absolute path to this skill>`
- `THREAD_ID=${CODEX_THREAD_ID:-manual-<timestamp-or-short-random-suffix>}`
- `TASK_SLUG=<sanitized task/deck slug>`
- `TOPIC_SLUG=<sanitized final deck filename slug>`

Select the remaining paths:

| Chat | Scratch workspace | Final PPTX |
| --- | --- | --- |
| Projectless | `$PWD/work/presentations/$TASK_SLUG` | User-requested path, otherwise `$PWD/outputs/$TOPIC_SLUG.pptx` |
| Project-backed | `$SCRATCH_ROOT/codex-presentations/$THREAD_ID/$TASK_SLUG` | User-requested path, repository convention, or `<project-root>/outputs/$TOPIC_SLUG.pptx` |

For project-backed chats, use an external scratch directory supplied by the
host. If none is supplied, compute `SCRATCH_ROOT` with
`node -p "require('node:os').tmpdir()"`; do not hardcode a platform-specific
temp path. Project-backed scratch must remain outside the repository.

An explicit user destination always wins. Set `OUTPUT_DIR` to the directory
containing `FINAL_PPTX`. If a projectless final is outside `outputs/`, an
optional copy under `outputs/` may be created for app surfacing, but the
requested path remains the primary result. Do not modify Git ignore settings
to conceal scratch files.

### Common workspace layout

After selecting `WORKSPACE`, set:

- `TMP_DIR=$WORKSPACE/tmp`
- `SLIDES_DIR=$TMP_DIR/slides`
- `PREVIEW_DIR=$TMP_DIR/preview`
- `LAYOUT_DIR=$TMP_DIR/layout`
- `ASSET_DIR=$TMP_DIR/assets`
- `QA_DIR=$TMP_DIR/qa`

Use absolute paths in scripts and handoffs. Put every generated file under
`$TMP_DIR` except `FINAL_PPTX` and any additional deliverables explicitly
requested by the user. Retain `$WORKSPACE` after delivery so follow-up turns
can inspect and reuse the prior work.

Use `.txt` for every generated intermediate prose artifact in `$TMP_DIR`,
including plans, source notes, prompt records, design notes, QA ledgers, and
fallback reasons. Reserve `.md` for installed skill/reference files such as
`SKILL.md`, `references/*.md`, and templates shipped with the skill. Do not
create generated planning files such as `slide-plan.md`.

## Route the Request Before Authoring

Choose the output path first:

1. **Existing native Google Slides deck**: use the Google Drive plugin's Google
   Slides skill. Do not round-trip it through a local PPTX unless the user asks.
2. **Net-new native Google Slides deck**: build and verify a local PPTX with
   this skill, then import it as described in Google Slides-Targeted Output.
3. **PowerPoint or local deck**: build or edit the PPTX with this skill.

For every deck built with this skill, choose exactly one visual route. The first
matching route wins:

1. **User reference or template skill**: if the user supplies a reference deck,
   asks to follow an existing deck, or invokes a template skill, use only that
   file as the visual source. An existing PPTX being edited also counts as the
   reference. Do not mix in Codex Grid or another template.
2. **Explicit custom formatting**: if there is no reference and the user asks
   for a theme, brand treatment, visual style, mood, or custom formatting,
   create the deck from scratch. Do not use Codex Grid.
3. **No visual direction**: use the AgentAPI Fast Path. Do not read Codex Grid
   files or run PPTX template-following mode.

User-provided references and explicit visual direction always take precedence
over Codex Grid.

## Google Slides-Targeted Output

For a net-new native Google Slides request, create and verify a local `.pptx`
with this skill first. The native Google Slides deliverable must then be
produced by the Google Drive plugin's presentation import action,
`mcp__codex_apps__google_drive_import_presentation`, with
`upload_mode: "native_google_slides"`.

Do not use Computer Use, Browser Use, blank-Google-Slides creation plus Google
Slides write APIs, or another direct-to-Slides construction path for net-new
Google Slides unless the user explicitly asks for that alternate workflow. If
the Google Drive plugin is unavailable, ask the user to install
`google-drive@openai-curated`. If the plugin is available but presentation
import is missing, ask the user to reinstall or refresh the Google Drive plugin
before continuing with the native Google Slides deliverable.

The local `.pptx` creation and native import workflow above applies only to
net-new Google Slides deliverables.

## Implementation

AgentAPI does not include Codex's private `@oai/artifact-tool` runtime. Do not
attempt to install, import, or bootstrap `@oai/artifact-tool` for AgentAPI deck
generation.

Use one of the public local authoring routes instead:

- Preferred: create a real `.pptx` under `$TMP_DIR` with `python3` and
  `python-pptx`, then register the final file with the Artifact tool using
  `file_path`.
- Alternative: create a real `.pptx` with Node.js and global `pptxgenjs`, then
  register the final file with the Artifact tool using `file_path`.

Before coding, verify the runtime quickly instead of exploring private
artifact-tool paths:

```bash
python3 "$CLAUDE_SKILL_DIR/../../skill-runtimes/office/check_office_runtime.py"
python3 - <<'PY'
import pptx
print("python-pptx ok")
PY
```

Create all helper scripts, notes, source summaries, and intermediate files under
`$TMP_DIR`. Export the final PowerPoint deck (`.pptx`) to `$FINAL_PPTX`, render
or inspect it with LibreOffice/Poppler when possible, then call Artifact only
for the final user-facing PPTX file.

## Template Following

Use template-following mode only when a user-provided source PPTX supplies the
layout, style, or template. Read `references/template-following.md`, use
`$TMP_DIR` from the Workspace section, and set
`TEMPLATE_PPTX="<absolute path to the user-provided PPTX>"`.

Preserve the source deck's typography, palette, spacing, layout, placeholders,
footers, page markers, and brand chrome unless the user explicitly asks to
restyle. Do not use template-following mode for a deck created from scratch.

Create:

- `$TMP_DIR/template-audit.txt`
- `$TMP_DIR/template-frame-map.json`
- `$TMP_DIR/deviation-log.txt`
- `$TMP_DIR/template-starter.pptx`

Keep `$TMP_DIR/source-notes.txt` for content and asset provenance.

Inspect the complete source deck:

```bash
node "$SKILL_DIR/template_following_scripts/inspect_template_deck.mjs" \
  --workspace "$TMP_DIR" \
  --pptx "$TEMPLATE_PPTX"
```

Map each output slide to an inherited source slide and identify element-level
`editTargets`. Then validate the map and build the starter deck:

```bash
node "$SKILL_DIR/template_following_scripts/validate_template_plan.mjs" \
  --workspace "$TMP_DIR" \
  --map "$TMP_DIR/template-frame-map.json"

node "$SKILL_DIR/template_following_scripts/prepare_template_starter_deck.mjs" \
  --workspace "$TMP_DIR" \
  --pptx "$TEMPLATE_PPTX" \
  --map "$TMP_DIR/template-frame-map.json" \
  --out "$TMP_DIR/template-starter.pptx" \
  --preview-dir "$TMP_DIR/template-starter-preview" \
  --layout-dir "$TMP_DIR/template-starter-layout" \
  --contact-sheet "$TMP_DIR/template-starter-contact-sheet.png"
```

Import `template-starter.pptx` with artifact-tool and edit only inherited
slides/objects unless the validated frame map explicitly allows an insertion.
If no source slide can support requested content without a parallel rebuild,
report the blocker and the closest viable source-slide options.

## QA Reminder

Before delivery, render every final slide and inspect the full-size previews or
contact sheet. Fix unintended overlap, clipping, wrapping, broken connectors,
unresolved placeholders, inconsistent footers/page markers, and chart/data
mismatches before exporting. Verify that researched claims and sourced assets
are traceable, and cite sources if research was used.

## Final Response

Return the final `.pptx` path and a link to it. Mention the sources cited or
used if research informed the deck. Do not attach scratch plans, previews,
layout JSON, or temporary assets unless the user asks for them.

## Codex App final response citations

When summarizing deck work in Codex App, cite only the final delivered PPTX.

Use slide citations when slide numbers come from the latest rendered or inspected final deck:

```text
::codex-file-citation{path="/abs/path/deck.pptx" artifact_kind="presentation" slide_number="3"}
```

Include `slide_id` only when artifact-tool inspection provides the exact stable `sl/...` ID and stable navigation matters:

```text
::codex-file-citation{path="/abs/path/deck.pptx" artifact_kind="presentation" slide_number="1" slide_id="sl/gs5z1kshq0xv"}
```

For a concrete chart, table, image, diagram, or callout, include `object_id` only when inspection provides the exact ID and you can add a useful label:

```text
::codex-file-citation{path="/abs/path/deck.pptx" artifact_kind="presentation" slide_number="1" slide_id="sl/gs5z1kshq0xv" object_id="ch/pz9t1r3ka8vn" label="ARR by segment chart"}
```

Do not cite internal previews, contact sheets, layout JSON, source notes, scratch files, builders, manifests, or QA outputs unless asked. If slide or object IDs are not reliable, cite the slide without object detail rather than guessing.
