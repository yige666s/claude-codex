#!/usr/bin/env python3
"""P2 sandbox-node protocol smoke. Simulation only; never executes code.

Reads the temporary node token from stdin so it need not appear in shell argv,
remote files or command history. Use only a development node and fake IDs.
"""
from __future__ import annotations

import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import hmac
import json
import secrets
import sys
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        return None


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", required=True, help="ngrok HTTPS endpoint or loopback HTTP for local tests")
    parser.add_argument("--revision", required=True, help="matches node -simulation-revision, not a request to enable execution")
    args = parser.parse_args()
    endpoint = args.endpoint.rstrip("/")
    parsed = urlsplit(endpoint)
    if (parsed.scheme != "https" and not (parsed.scheme == "http" and parsed.hostname in {"127.0.0.1", "::1"})) or parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path:
        parser.error("endpoint must be HTTPS, except numeric loopback HTTP, without path/credentials/query")
    token = sys.stdin.readline().strip().encode("ascii")
    if len(token) < 32:
        parser.error("read a temporary token of at least 32 bytes from stdin")
    opener = build_opener(NoRedirect)

    def request(path: str, payload: dict | None = None, signed: bool = False, authorized: bool = True) -> tuple[int, dict | None]:
        method = "POST" if payload is not None else "GET"
        body = json.dumps(payload, separators=(",", ":")).encode() if payload is not None else b""
        headers = {"ngrok-skip-browser-warning": "true"}
        if signed:
            timestamp = str(int(time.time()))
            nonce = secrets.token_hex(16)
            canonical = f"{method}\n{path}\n{timestamp}\n{nonce}\n{hashlib.sha256(body).hexdigest()}".encode()
            headers.update({
                "X-Sandbox-Timestamp": timestamp,
                "X-Sandbox-Nonce": nonce,
                "X-Sandbox-Signature": hmac.new(token, canonical, hashlib.sha256).hexdigest(),
            })
        elif authorized:
            headers["Authorization"] = "Bearer " + token.decode("ascii")
        if payload is not None:
            headers["Content-Type"] = "application/json"
        req = Request(endpoint + path, data=body if payload is not None else None, headers=headers, method=method)
        try:
            with opener.open(req, timeout=15) as response:
                raw = response.read(16384)
                return response.status, json.loads(raw) if raw else None
        except HTTPError as error:
            return error.code, None

    def expect(label: str, actual: int, wanted: int) -> None:
        print(f"{label}: HTTP {actual} (expected {wanted})")
        if actual != wanted:
            raise RuntimeError(f"{label} failed")

    anonymous, _ = request("/v1/health", authorized=False)
    expect("anonymous health", anonymous, 401)
    health_status, health = request("/v1/health")
    expect("authenticated health", health_status, 200)
    if not health or health.get("protocol_version") != "v1":
        raise RuntimeError("node protocol version mismatch")
    caps_status, caps = request("/v1/capabilities")
    expect("capabilities", caps_status, 200)
    if not caps or caps.get("accepting_jobs") is not False or caps.get("drivers") != []:
        raise RuntimeError("node unexpectedly accepts real jobs")

    job_id = "p2-smoke-" + secrets.token_hex(8)
    spec = {
        "job_id": job_id,
        "user_id": "p2-smoke-only",
        "policy_revision": args.revision,
        "idempotency_key": "p2-" + secrets.token_hex(8),
        "deadline": (datetime.now(timezone.utc) + timedelta(minutes=2)).isoformat(),
    }
    status, lease = request("/v1/sim/prepare", spec, signed=True)
    expect("simulation prepare", status, 200)
    if not lease or lease.get("state") != "prepared" or not lease.get("lease_id"):
        raise RuntimeError("invalid simulated lease")
    repeat_status, repeated = request("/v1/sim/prepare", spec, signed=True)
    expect("idempotent prepare", repeat_status, 200)
    if not repeated or repeated.get("lease_id") != lease["lease_id"]:
        raise RuntimeError("duplicate simulated lease")
    ref = {"lease_id": lease["lease_id"], "user_id": spec["user_id"]}
    exec_status, execution = request("/v1/sim/exec", ref, signed=True)
    expect("simulation exec", exec_status, 200)
    if not execution or execution.get("code_executed") is not False:
        raise RuntimeError("unexpected code execution")
    collect_status, collected = request("/v1/sim/collect", ref, signed=True)
    expect("empty collect", collect_status, 200)
    if not collected or collected.get("artifacts") != []:
        raise RuntimeError("simulation returned artifacts")
    cancel_status, _ = request("/v1/sim/cancel", ref, signed=True)
    expect("cancel", cancel_status, 200)
    dispose_status, _ = request("/v1/sim/dispose", ref, signed=True)
    expect("dispose", dispose_status, 200)
    real_status, _ = request("/v1/exec")
    expect("real execution unavailable", real_status, 404)
    print("P2 simulation smoke passed; no code was executed.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (URLError, RuntimeError, ValueError) as error:
        print(f"P2 smoke failed: {error}", file=sys.stderr)
        raise SystemExit(1)
