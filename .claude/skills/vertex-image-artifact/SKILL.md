---
name: "vertex-image-artifact"
description: "Generate one image with Vertex AI Imagen and save it as a generated artifact. Triggers include: 生成图片, 生成以下图片, 帮我生成图片, 画一张, 生图, generate image, create image, render image."
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
    policy:
      allowed_env:
        - VERTEX_PROJECT_ID
        - GOOGLE_CLOUD_PROJECT
        - GCP_PROJECT
        - VERTEX_LOCATION
        - GOOGLE_CLOUD_LOCATION
        - VERTEX_ACCESS_TOKEN
        - GOOGLE_OAUTH_ACCESS_TOKEN
        - GOOGLE_ACCESS_TOKEN
        - GOOGLE_APPLICATION_CREDENTIALS
        - GOOGLE_APPLICATION_CREDENTIALS_JSON
        - VERTEX_SERVICE_ACCOUNT_FILE
        - VERTEX_SERVICE_ACCOUNT_JSON
        - VERTEX_IMAGE_MODEL
        - VERTEX_IMAGE_ASPECT_RATIO
      network_allowlist:
        - aiplatform.googleapis.com
        - oauth2.googleapis.com
        - googleapis.com
      artifact_content_types:
        - image/png
      sandbox:
        runner: docker
        image: python:3.12-slim
        network: bridge
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
        - GOOGLE_APPLICATION_CREDENTIALS
        - GOOGLE_APPLICATION_CREDENTIALS_JSON
        - VERTEX_SERVICE_ACCOUNT_FILE
        - VERTEX_SERVICE_ACCOUNT_JSON
        - VERTEX_IMAGE_MODEL
        - VERTEX_IMAGE_ASPECT_RATIO
---

# Vertex Image Artifact Test

Generate a simple test image with Vertex AI Imagen, then save the generated PNG as an artifact.

The shell step below calls the Vertex Imagen REST `predict` API and writes the PNG into the current user's workspace. It expects `VERTEX_PROJECT_ID`, `VERTEX_LOCATION`, and either service account credentials from `GOOGLE_APPLICATION_CREDENTIALS` / `GOOGLE_APPLICATION_CREDENTIALS_JSON`, a Vertex OAuth access token from `VERTEX_ACCESS_TOKEN`, `GOOGLE_OAUTH_ACCESS_TOKEN`, `GOOGLE_ACCESS_TOKEN`, or local `gcloud auth print-access-token`.

Vertex Imagen generation requires the request to include `instances[0].prompt`. This skill supplies the required `parameters.sampleCount` as `1`, sends `parameters.outputOptions.mimeType` as `image/png`, and defaults `parameters.aspectRatio` to `1:1` when the user does not specify one.

The script parses image options from the user prompt before calling Vertex Imagen:

- `--ar 1:1`, `--ar=1:1`, `--aspect-ratio 1:1`, or `--aspect-ratio=1:1`
- Supported aspect ratios are `1:1`, `3:4`, `4:3`, `16:9`, and `9:16`
- Unsupported numeric ratios are normalized to the nearest supported Vertex Imagen ratio and reported as `aspect_ratio_note`
- Parsed flags are removed from the prompt sent to Vertex Imagen

```!
python3 "${CLAUDE_SKILL_DIR}/generate_vertex_image.py" <<'VERTEX_IMAGE_PROMPT'
$ARGUMENTS
VERTEX_IMAGE_PROMPT
```

Use the `Artifact` tool exactly once with the `artifact_file_path` printed above. The value is intentionally relative to the current workspace; do not rewrite it as an absolute path.

- `filename`: use the printed `filename`
- `content_type`: `image/png`
- `file_path`: use the printed `artifact_file_path`

If the shell output contains `skill_error:`, do not call the `Artifact` tool. Reply in the user's language with the friendly error from `skill_error:` and, when useful, a concise next step. Do not expose raw Vertex JSON, stack traces, shell commands, auth tokens, artifact IDs, object paths, or download paths to the user.

The shell output may contain `skill_log: {...}` diagnostic lines. These are for backend execution history only. Do not summarize, quote, or expose them to the user.

After the `Artifact` tool succeeds, do not expose raw JSON, artifact IDs, object paths, or download paths to the user. Use the tool result only as internal context, then reply in natural language that the image is ready and can be viewed in the Artifacts panel. Mention the generated filename only if it helps the user identify the asset, and offer a concise next step such as revising the prompt or generating another variant.
