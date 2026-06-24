---
name: "pdf"
description: "Read, create, inspect, render, and verify PDF files where visual layout matters. Use Poppler rendering plus Python tools such as reportlab, pdfplumber, and pypdf for generation and extraction."
user-invocable: true
allowed-tools: ["Artifact", "Bash"]
shell: bash
metadata:
  product:
    version: "26.623.12021"
    category: "Documents"
    icon: "PDF"
  agentapi:
    run_as_job: true
    long_running: true
    produces_artifacts: true
    policy:
      artifact_content_types:
        - application/pdf
        - image/png
      allowed_tools:
        - Artifact
        - Bash
      allowed_env:
        - HOME
        - TMPDIR
        - AGENT_WORKSPACE_DIR
        - CLAUDE_SKILL_DIR
      sandbox:
        runner: local
        network: none
---

# PDF Skill

## AgentAPI Runtime Adaptation

This project provides the PDF runtime in the AgentAPI container:
`python3`, `reportlab`, `pdfplumber`, `pypdf`, `pdf2image`, and Poppler
(`pdftoppm`, `pdfinfo`). Run
`python3 ../../skill-runtimes/office/check_office_runtime.py`
when you need to confirm the runtime before producing a PDF artifact.

## When To Use

- Read or review PDF content where layout and visuals matter.
- Create PDFs programmatically with reliable formatting.
- Validate final rendering before delivery.

## Workflow

1. Prefer visual review: render PDF pages to PNGs and inspect them.
   - Use `pdftoppm` from the bundled runtime or system Poppler when available.
   - If unavailable, install Poppler or ask the user to review the output locally.
2. Use `reportlab` to generate PDFs when creating new documents.
3. Use `pdfplumber` or `pypdf` for text extraction and quick checks; do not rely on text extraction for layout fidelity.
4. After each meaningful update, re-render pages and verify alignment, spacing, and legibility.

## Temp And Output Conventions

- Use `tmp/pdfs/` for intermediate files; delete them when done.
- Write final artifacts under `output/pdf/` when working in this repo.
- Keep filenames stable and descriptive.

## Dependencies

Prefer the Codex bundled workspace/runtime dependencies when available. The primary runtime is expected to include:

- Python packages: `reportlab`, `pdfplumber`, `pypdf`
- Rendering tools: `pdftoppm` and `pdfinfo` from Poppler

If a dependency is missing, install only what is needed.

Python packages:

```bash
uv pip install reportlab pdfplumber pypdf
```

If `uv` is unavailable:

```bash
python3 -m pip install reportlab pdfplumber pypdf
```

System tools for rendering:

```bash
# macOS (Homebrew)
brew install poppler

# Ubuntu/Debian
sudo apt-get install -y poppler-utils
```

If installation is not possible in this environment, tell the user which dependency is missing and how to install it locally.

## Environment

No required environment variables.

## Rendering Command

```bash
pdftoppm -png "$INPUT_PDF" "$OUTPUT_PREFIX"
```

## Quality Expectations

- Maintain polished visual design: consistent typography, spacing, margins, and section hierarchy.
- Avoid rendering issues: clipped text, overlapping elements, broken tables, black squares, or unreadable glyphs.
- Charts, tables, and images must be sharp, aligned, and clearly labeled.
- Use ASCII hyphens only. Avoid U+2011 and other Unicode dashes.
- Citations and references must be human-readable; never leave tool tokens or placeholder strings.

## Final Checks

- Do not deliver until the latest PNG inspection shows zero visual or formatting defects.
- Confirm headers, footers, page numbering, and section transitions look polished.
- Keep intermediate files organized or remove them after final approval.
