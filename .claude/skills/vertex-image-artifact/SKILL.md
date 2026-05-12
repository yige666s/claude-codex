---
name: "vertex-image-artifact"
description: "Generate one image with Vertex AI Imagen and save it as a generated artifact."
user-invocable: true
argument-hint: "<image prompt>"
allowed-tools: ["Artifact", "Bash(python3 *)"]
shell: bash
metadata:
  product:
    version: "0.1.0"
    category: "Image Generation"
    icon: "IMG"
  agentapi:
    run_as_job: true
    produces_artifacts: true
  openclaw:
    requires:
      env:
        - VERTEX_PROJECT_ID
        - GOOGLE_CLOUD_PROJECT
        - GCP_PROJECT
        - VERTEX_LOCATION
        - GOOGLE_CLOUD_LOCATION
        - VERTEX_ACCESS_TOKEN
        - GOOGLE_OAUTH_ACCESS_TOKEN
        - GOOGLE_ACCESS_TOKEN
        - VERTEX_IMAGE_MODEL
        - VERTEX_IMAGE_ASPECT_RATIO
---

# Vertex Image Artifact Test

Generate a simple test image with Vertex AI Imagen, then save the generated PNG as an artifact.

The shell step below calls the Vertex Imagen REST `predict` API and writes the PNG into the current user's workspace. It expects `VERTEX_PROJECT_ID`, `VERTEX_LOCATION`, and a Vertex OAuth access token from `VERTEX_ACCESS_TOKEN`, `GOOGLE_OAUTH_ACCESS_TOKEN`, `GOOGLE_ACCESS_TOKEN`, or local `gcloud auth print-access-token`.

```!
python3 "${CLAUDE_SKILL_DIR}/generate_vertex_image.py" <<'VERTEX_IMAGE_PROMPT'
$ARGUMENTS
VERTEX_IMAGE_PROMPT
```

Use the `Artifact` tool exactly once with the `artifact_file_path` printed above. The value is intentionally relative to the current workspace; do not rewrite it as an absolute path.

- `filename`: use the printed `filename`
- `content_type`: `image/png`
- `file_path`: use the printed `artifact_file_path`

After the `Artifact` tool succeeds, do not expose raw JSON, artifact IDs, object paths, or download paths to the user. Use the tool result only as internal context, then reply in natural language that the image is ready and can be viewed in the Artifacts panel. Mention the generated filename only if it helps the user identify the asset, and offer a concise next step such as revising the prompt or generating another variant.
