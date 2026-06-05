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

DEFAULT_PROMPT = "a small friendly robot painting a tiny test image, clean product illustration"
DEFAULT_ASPECT_RATIO = "1:1"
DEFAULT_VERTEX_IMAGE_MODEL = "imagen-3.0-generate-002"
SUPPORTED_ASPECT_RATIOS = ("1:1", "3:4", "4:3", "16:9", "9:16")
ASPECT_RATIO_FLAGS = ("--ar", "--aspect-ratio", "--aspect_ratio")


def utc_now() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def prompt_fingerprint(prompt: str) -> str:
    return hashlib.sha256(prompt.encode("utf-8", errors="replace")).hexdigest()[:16]


def log_event(event: str, **fields: object) -> None:
    payload = {
        "ts": utc_now(),
        "skill": "vertex-image-artifact",
        "event": event,
        **fields,
    }
    try:
        print("skill_log: " + json.dumps(payload, ensure_ascii=False, default=str))
    except Exception:
        # Logging must never affect image generation.
        return


def log_exception(event: str, exc: BaseException, **fields: object) -> None:
    log_event(
        event,
        error_type=type(exc).__name__,
        error=str(exc)[:1000],
        **fields,
    )


class SkillUserError(Exception):
    def __init__(self, message: str, kind: str = "generation_failed"):
        super().__init__(message)
        self.message = message
        self.kind = kind


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
    missing = [name for name in ("client_email", "private_key") if not credentials.get(name)]
    if missing:
        raise SkillUserError(
            "图片服务认证配置不完整，请联系管理员检查 Vertex service account。",
            "auth_config",
        )
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
        raise SkillUserError(
            "图片服务认证没有返回可用令牌，请联系管理员检查 Vertex service account。",
            "auth_config",
        )
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
            raise SkillUserError(
                "图片服务认证已失效，并且无法自动刷新。请稍后再试或联系管理员检查 Vertex 凭据。",
                "auth_rejected",
            ) from exc
        token = env_first("VERTEX_ACCESS_TOKEN", "GOOGLE_OAUTH_ACCESS_TOKEN", "GOOGLE_ACCESS_TOKEN")
        if token:
            return token
        raise SkillUserError(
            "图片服务认证配置不可用，请联系管理员检查 Vertex service account。",
            "auth_config",
        ) from exc
    token = env_first("VERTEX_ACCESS_TOKEN", "GOOGLE_OAUTH_ACCESS_TOKEN", "GOOGLE_ACCESS_TOKEN")
    if token:
        return token
    raise SkillUserError(
        "图片服务认证令牌为空，请联系管理员检查 Vertex service account。",
        "auth_config",
    )


def aspect_ratio_number(value: str) -> float | None:
    match = re.fullmatch(r"\s*(\d+(?:\.\d+)?)\s*[:x/]\s*(\d+(?:\.\d+)?)\s*", value, re.IGNORECASE)
    if not match:
        return None
    width = float(match.group(1))
    height = float(match.group(2))
    if width <= 0 or height <= 0:
        return None
    return width / height


def closest_supported_aspect_ratio(value: str) -> str:
    requested = aspect_ratio_number(value)
    if requested is None:
        return DEFAULT_ASPECT_RATIO
    return min(
        SUPPORTED_ASPECT_RATIOS,
        key=lambda supported: abs((aspect_ratio_number(supported) or 1.0) - requested),
    )


def normalize_aspect_ratio(value: str) -> tuple[str, str]:
    value = value.strip().lower().replace(" ", "")
    if not value:
        return DEFAULT_ASPECT_RATIO, ""
    value = value.replace("×", "x")
    normalized = re.sub(r"^(\d+(?:\.\d+)?)[x/](\d+(?:\.\d+)?)$", r"\1:\2", value)
    if normalized in SUPPORTED_ASPECT_RATIOS:
        return normalized, ""
    fallback = closest_supported_aspect_ratio(normalized)
    supported = ", ".join(SUPPORTED_ASPECT_RATIOS)
    return (
        fallback,
        f"Requested aspect ratio {value} is not supported by Vertex Imagen; using {fallback}. Supported values: {supported}.",
    )


def parse_prompt_options(raw_prompt: str) -> tuple[str, str, str]:
    prompt = raw_prompt.strip()
    aspect_ratio = env_first("VERTEX_IMAGE_ASPECT_RATIO") or DEFAULT_ASPECT_RATIO
    note = ""

    for flag in ASPECT_RATIO_FLAGS:
        equals_pattern = re.compile(rf"(?<!\S){re.escape(flag)}=(\S+)")
        match = equals_pattern.search(prompt)
        if match:
            aspect_ratio = match.group(1)
            prompt = (prompt[: match.start()] + prompt[match.end() :]).strip()
            break

        spaced_pattern = re.compile(rf"(?<!\S){re.escape(flag)}\s+(\S+)")
        match = spaced_pattern.search(prompt)
        if match:
            aspect_ratio = match.group(1)
            prompt = (prompt[: match.start()] + prompt[match.end() :]).strip()
            break

    aspect_ratio, note = normalize_aspect_ratio(aspect_ratio)
    prompt = re.sub(r"\s+", " ", prompt).strip() or DEFAULT_PROMPT
    return prompt, aspect_ratio, note


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
        raise SkillUserError(
            "图片生成服务返回了结果，但没有包含可保存的图片内容。请换一个描述再试。",
            "empty_image",
        )
    return base64.b64decode(encoded), mime_type


def vertex_error_message(status_code: int, detail: str) -> tuple[str, str]:
    message = ""
    try:
        payload = json.loads(detail)
        message = str((payload.get("error") or {}).get("message") or "")
    except json.JSONDecodeError:
        message = detail
    lowered = message.lower()

    if status_code in (401, 403):
        return (
            "图片服务认证或权限配置异常，请稍后再试，或联系管理员检查 Vertex service account 权限。",
            "auth_rejected",
        )
    if status_code == 429:
        return (
            "图片生成服务当前达到配额或频率上限，请稍后再试。",
            "rate_limited",
        )
    if status_code >= 500:
        return (
            "Vertex Imagen 服务暂时不可用，请稍后再试。",
            "service_unavailable",
        )
    if status_code == 400 and "aspect ratio" in lowered:
        supported = ", ".join(SUPPORTED_ASPECT_RATIOS)
        return (
            f"图片比例没有通过 Vertex Imagen 校验。请使用这些比例之一：{supported}。",
            "invalid_aspect_ratio",
        )
    if status_code == 400:
        return (
            "图片请求参数没有通过 Vertex Imagen 校验，请简化描述或改用支持的图片比例后再试。",
            "invalid_request",
        )
    return (
        "图片生成失败，请调整描述后再试。",
        "generation_failed",
    )


def main() -> int:
    started = time.time()
    prompt, aspect_ratio, aspect_ratio_note = parse_prompt_options(sys.stdin.read())

    project_id = env_first("VERTEX_PROJECT_ID", "GOOGLE_CLOUD_PROJECT", "GCP_PROJECT")
    location = env_first("VERTEX_LOCATION", "GOOGLE_CLOUD_LOCATION") or "us-central1"
    model = env_first("VERTEX_IMAGE_MODEL") or DEFAULT_VERTEX_IMAGE_MODEL
    prompt_hash = prompt_fingerprint(prompt)
    log_event(
        "start",
        prompt_hash=prompt_hash,
        prompt_length=len(prompt),
        aspect_ratio=aspect_ratio,
        aspect_ratio_note=aspect_ratio_note,
        project_configured=bool(project_id),
        provider="vertex",
        location=location,
        model=model,
    )
    if not project_id:
        log_event("config_error", kind="missing_project", prompt_hash=prompt_hash)
        raise SkillUserError(
            "图片服务缺少 Vertex 项目配置，请联系管理员检查 VERTEX_PROJECT_ID。",
            "missing_project",
        )

    endpoint = (
        f"https://{location}-aiplatform.googleapis.com/v1/"
        f"projects/{project_id}/locations/{location}/publishers/google/models/{model}:predict"
    )
    body = {
        "instances": [{"prompt": prompt}],
        "parameters": {
            "sampleCount": 1,
            "aspectRatio": aspect_ratio,
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
            log_event(
                "vertex_response",
                prompt_hash=prompt_hash,
                provider="vertex",
                model=model,
                status=getattr(response, "status", 200),
                duration_ms=round((time.time() - started) * 1000),
            )
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        message, kind = vertex_error_message(exc.code, detail)
        log_event(
            "vertex_http_error",
            prompt_hash=prompt_hash,
            provider="vertex",
            model=model,
            status=exc.code,
            kind=kind,
            error_message=message,
            retry=exc.code == 401,
            duration_ms=round((time.time() - started) * 1000),
        )
        if exc.code != 401:
            raise SkillUserError(message, kind) from exc
        try:
            with urllib.request.urlopen(build_request(access_token(refresh=True)), timeout=120) as response:
                payload = json.loads(response.read().decode("utf-8"))
                log_event(
                    "vertex_response_after_refresh",
                    prompt_hash=prompt_hash,
                    provider="vertex",
                    model=model,
                    status=getattr(response, "status", 200),
                    duration_ms=round((time.time() - started) * 1000),
                )
        except urllib.error.HTTPError as retry_exc:
            detail = retry_exc.read().decode("utf-8", errors="replace")
            message, kind = vertex_error_message(retry_exc.code, detail)
            log_event(
                "vertex_retry_http_error",
                prompt_hash=prompt_hash,
                provider="vertex",
                model=model,
                status=retry_exc.code,
                kind=kind,
                error_message=message,
                duration_ms=round((time.time() - started) * 1000),
            )
            raise SkillUserError(message, kind) from retry_exc
        except Exception as retry_exc:
            log_exception(
                "vertex_retry_exception",
                retry_exc,
                prompt_hash=prompt_hash,
                provider="vertex",
                model=model,
                duration_ms=round((time.time() - started) * 1000),
            )
            raise
    except Exception as exc:
        log_exception(
            "vertex_request_exception",
            exc,
            prompt_hash=prompt_hash,
            provider="vertex",
            model=model,
            duration_ms=round((time.time() - started) * 1000),
        )
        raise

    predictions = payload.get("predictions") or []
    if not predictions:
        log_event("empty_response", prompt_hash=prompt_hash, duration_ms=round((time.time() - started) * 1000))
        raise SkillUserError(
            "图片生成服务没有返回图片结果，请换一个描述再试。",
            "empty_response",
        )

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
    log_event(
        "success",
        prompt_hash=prompt_hash,
        provider="vertex",
        model=model,
        filename=filename,
        content_type=mime_type,
        size_bytes=len(image_bytes),
        aspect_ratio=aspect_ratio,
        duration_ms=round((time.time() - started) * 1000),
    )

    print(f"output_file: {output_file}")
    print(f"artifact_file_path: {artifact_file_path}")
    print(f"filename: {filename}")
    print(f"content_type: {mime_type}")
    print(f"model: {model}")
    print(f"aspect_ratio: {aspect_ratio}")
    if aspect_ratio_note:
        print(f"aspect_ratio_note: {aspect_ratio_note}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SkillUserError as exc:
        log_event("user_error", kind=exc.kind, message=exc.message)
        print(f"skill_error: {exc.message}")
        print(f"error_kind: {exc.kind}")
        raise SystemExit(0)
    except Exception as exc:
        log_exception("internal_error", exc)
        print("skill_error: 图片生成在准备阶段失败，请稍后再试或联系管理员检查服务配置。")
        print("error_kind: internal")
        raise SystemExit(0)
