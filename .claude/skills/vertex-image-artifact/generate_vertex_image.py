#!/usr/bin/env python3
"""Small Vertex Imagen smoke-test helper for the web agent artifact flow."""

from __future__ import annotations

import base64
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


def env_first(*names: str) -> str:
    for name in names:
        value = os.environ.get(name, "").strip()
        if value:
            return value
    return ""


def gcloud_access_token() -> str:
    return subprocess.check_output(
        ["gcloud", "auth", "print-access-token"],
        text=True,
        stderr=subprocess.DEVNULL,
    ).strip()


def access_token(refresh: bool = False) -> str:
    if not refresh:
        token = env_first("VERTEX_ACCESS_TOKEN", "GOOGLE_OAUTH_ACCESS_TOKEN", "GOOGLE_ACCESS_TOKEN")
        if token:
            return token
    try:
        token = gcloud_access_token()
        if token:
            return token
    except Exception as exc:  # pragma: no cover - depends on local gcloud
        if refresh:
            raise SystemExit(
                "Vertex token was rejected and gcloud auth print-access-token is unavailable"
            ) from exc
        token = env_first("VERTEX_ACCESS_TOKEN", "GOOGLE_OAUTH_ACCESS_TOKEN", "GOOGLE_ACCESS_TOKEN")
        if token:
            return token
        raise SystemExit(
            "Missing Vertex access token and failed to run gcloud auth print-access-token"
        ) from exc
    token = env_first("VERTEX_ACCESS_TOKEN", "GOOGLE_OAUTH_ACCESS_TOKEN", "GOOGLE_ACCESS_TOKEN")
    if token:
        return token
    raise SystemExit("Vertex access token is empty")


def safe_name(prompt: str) -> str:
    text = re.sub(r"[^a-zA-Z0-9]+", "-", prompt.lower()).strip("-")
    if not text:
        text = "vertex-image"
    return text[:48]


def prediction_image(prediction: dict) -> tuple[bytes, str]:
    encoded = prediction.get("bytesBase64Encoded")
    mime_type = prediction.get("mimeType") or "image/png"
    if not encoded and isinstance(prediction.get("image"), dict):
        image = prediction["image"]
        encoded = image.get("bytesBase64Encoded")
        mime_type = image.get("mimeType") or mime_type
    if not encoded:
        raise SystemExit("Vertex response did not contain bytesBase64Encoded")
    return base64.b64decode(encoded), mime_type


def main() -> int:
    prompt = sys.stdin.read().strip()
    if not prompt:
        prompt = "a small friendly robot painting a tiny test image, clean product illustration"

    project_id = env_first("VERTEX_PROJECT_ID", "GOOGLE_CLOUD_PROJECT", "GCP_PROJECT")
    location = env_first("VERTEX_LOCATION", "GOOGLE_CLOUD_LOCATION") or "us-central1"
    model = env_first("VERTEX_IMAGE_MODEL") or "imagen-4.0-generate-001"
    if not project_id:
        raise SystemExit("VERTEX_PROJECT_ID or GOOGLE_CLOUD_PROJECT is required")

    endpoint = (
        f"https://{location}-aiplatform.googleapis.com/v1/"
        f"projects/{project_id}/locations/{location}/publishers/google/models/{model}:predict"
    )
    body = {
        "instances": [{"prompt": prompt}],
        "parameters": {
            "sampleCount": 1,
            "aspectRatio": os.environ.get("VERTEX_IMAGE_ASPECT_RATIO", "1:1"),
            "enhancePrompt": True,
            "outputOptions": {"mimeType": "image/png"},
        },
    }
    def build_request(token: str) -> urllib.request.Request:
        return urllib.request.Request(
            endpoint,
            data=json.dumps(body).encode("utf-8"),
            headers={
                "Authorization": f"Bearer {token}",
                "Content-Type": "application/json",
            },
            method="POST",
        )

    try:
        with urllib.request.urlopen(build_request(access_token()), timeout=120) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        if exc.code != 401:
            detail = exc.read().decode("utf-8", errors="replace")
            raise SystemExit(f"Vertex Imagen request failed: HTTP {exc.code}: {detail}") from exc
        try:
            with urllib.request.urlopen(build_request(access_token(refresh=True)), timeout=120) as response:
                payload = json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as retry_exc:
            detail = retry_exc.read().decode("utf-8", errors="replace")
            raise SystemExit(f"Vertex Imagen request failed after token refresh: HTTP {retry_exc.code}: {detail}") from retry_exc

    predictions = payload.get("predictions") or []
    if not predictions:
        raise SystemExit(f"Vertex response did not contain predictions: {json.dumps(payload)[:500]}")

    image_bytes, mime_type = prediction_image(predictions[0])
    if mime_type != "image/png":
        suffix = ".jpg" if mime_type == "image/jpeg" else ".bin"
    else:
        suffix = ".png"

    workspace = Path(env_first("AGENT_WORKSPACE_DIR") or os.getcwd())
    output_dir = workspace / "generated-artifacts"
    output_dir.mkdir(parents=True, exist_ok=True)
    filename = f"vertex-image-{int(time.time())}-{safe_name(prompt)}{suffix}"
    output_file = output_dir / filename
    output_file.write_bytes(image_bytes)
    artifact_file_path = f"generated-artifacts/{filename}"

    print(f"output_file: {output_file}")
    print(f"artifact_file_path: {artifact_file_path}")
    print(f"filename: {filename}")
    print(f"content_type: {mime_type}")
    print(f"model: {model}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
