#!/usr/bin/env python3
"""Small Vertex Imagen smoke-test helper for the web agent artifact flow."""

from __future__ import annotations

import base64
import hashlib
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
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


def b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


class DERReader:
    def __init__(self, data: bytes):
        self.data = data
        self.pos = 0

    def read_tlv(self) -> tuple[int, bytes]:
        if self.pos >= len(self.data):
            raise ValueError("unexpected end of DER")
        tag = self.data[self.pos]
        self.pos += 1
        if self.pos >= len(self.data):
            raise ValueError("missing DER length")
        length = self.data[self.pos]
        self.pos += 1
        if length & 0x80:
            count = length & 0x7F
            if count == 0 or self.pos + count > len(self.data):
                raise ValueError("invalid DER length")
            length = int.from_bytes(self.data[self.pos : self.pos + count], "big")
            self.pos += count
        if self.pos + length > len(self.data):
            raise ValueError("DER value exceeds input")
        value = self.data[self.pos : self.pos + length]
        self.pos += length
        return tag, value


def pem_to_der(pem_text: str) -> bytes:
    body = []
    for line in pem_text.strip().splitlines():
        line = line.strip()
        if not line or line.startswith("-----"):
            continue
        body.append(line)
    if not body:
        raise ValueError("private_key is not PEM encoded")
    return base64.b64decode("".join(body))


def parse_rsa_private_key(pem_text: str) -> tuple[int, int]:
    der = pem_to_der(pem_text)
    tag, value = DERReader(der).read_tlv()
    if tag != 0x30:
        raise ValueError("private_key is not a DER sequence")

    # PKCS#8 wraps the PKCS#1 RSA key in an octet string.
    reader = DERReader(value)
    items: list[tuple[int, bytes]] = []
    while reader.pos < len(reader.data):
        items.append(reader.read_tlv())
    if len(items) >= 3 and items[2][0] == 0x04:
        tag, value = DERReader(items[2][1]).read_tlv()
        if tag != 0x30:
            raise ValueError("PKCS#8 private key payload is not a sequence")

    reader = DERReader(value)
    integers: list[int] = []
    while reader.pos < len(reader.data):
        tag, raw = reader.read_tlv()
        if tag == 0x02:
            integers.append(int.from_bytes(raw, "big", signed=False))
    if len(integers) < 4:
        raise ValueError("RSA private key is missing modulus/private exponent")
    modulus = integers[1]
    private_exponent = integers[3]
    return modulus, private_exponent


def rsa_sha256_sign(private_key_pem: str, data: bytes) -> bytes:
    modulus, private_exponent = parse_rsa_private_key(private_key_pem)
    digest = hashlib.sha256(data).digest()
    digest_info = bytes.fromhex("3031300d060960864801650304020105000420") + digest
    key_size = (modulus.bit_length() + 7) // 8
    padding_len = key_size - len(digest_info) - 3
    if padding_len < 8:
        raise ValueError("RSA key is too small for SHA-256 signature")
    encoded = b"\x00\x01" + (b"\xff" * padding_len) + b"\x00" + digest_info
    signature_int = pow(int.from_bytes(encoded, "big"), private_exponent, modulus)
    return signature_int.to_bytes(key_size, "big")


def service_account_credentials() -> dict | None:
    raw_json = env_first("GOOGLE_APPLICATION_CREDENTIALS_JSON", "VERTEX_SERVICE_ACCOUNT_JSON")
    if raw_json:
        return json.loads(raw_json)
    path = env_first("GOOGLE_APPLICATION_CREDENTIALS", "VERTEX_SERVICE_ACCOUNT_FILE")
    if path:
        return json.loads(Path(path).read_text(encoding="utf-8"))
    return None


def service_account_access_token() -> str:
    credentials = service_account_credentials()
    if not credentials:
        return ""
    token_uri = credentials.get("token_uri") or "https://oauth2.googleapis.com/token"
    now = int(time.time())
    header = {"alg": "RS256", "typ": "JWT"}
    claims = {
        "iss": credentials["client_email"],
        "scope": "https://www.googleapis.com/auth/cloud-platform",
        "aud": token_uri,
        "iat": now,
        "exp": now + 3600,
    }
    unsigned = f"{b64url(json.dumps(header, separators=(',', ':')).encode())}.{b64url(json.dumps(claims, separators=(',', ':')).encode())}"
    signature = rsa_sha256_sign(credentials["private_key"], unsigned.encode("ascii"))
    assertion = f"{unsigned}.{b64url(signature)}"
    body = urllib.parse.urlencode(
        {
            "grant_type": "urn:ietf:params:oauth:grant-type:jwt-bearer",
            "assertion": assertion,
        }
    ).encode("utf-8")
    request = urllib.request.Request(
        token_uri,
        data=body,
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        payload = json.loads(response.read().decode("utf-8"))
    token = str(payload.get("access_token") or "").strip()
    if not token:
        raise SystemExit("Service account token response did not include access_token")
    return token


def access_token(refresh: bool = False) -> str:
    if not refresh:
        token = env_first("VERTEX_ACCESS_TOKEN", "GOOGLE_OAUTH_ACCESS_TOKEN", "GOOGLE_ACCESS_TOKEN")
        if token:
            return token
    token = service_account_access_token()
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
