#!/usr/bin/env python3
"""Run only on a controlled Linux Node with a private test key and loopback endpoint."""
import hashlib
import hmac
import json
import os
import sys
import time
import urllib.error
import urllib.request

KEY = "/etc/claude-sandbox-node/node.key"
STATE = "/var/lib/claude-sandbox-node/nonce-smoke.json"
URL = "http://127.0.0.1:18080/v1/tasks/prepare"
BODY = b'{"invalid":true}'


def request(payload):
    req = urllib.request.Request(URL, BODY, payload, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=8) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code


def main():
    if len(sys.argv) != 2 or sys.argv[1] not in {"first", "replay"}:
        raise SystemExit("usage: sandbox_nonce_smoke.py first|replay")
    if sys.argv[1] == "first":
        key = open(KEY, "rb").read().strip()
        timestamp = str(int(time.time()))
        nonce = os.urandom(16).hex()
        path = "/v1/tasks/prepare"
        material = f"POST\n{path}\n{timestamp}\n{nonce}\n{hashlib.sha256(BODY).hexdigest()}".encode()
        signature = hmac.new(key, material, hashlib.sha256).hexdigest()
        headers = {
            "Content-Type": "application/json",
            "X-Sandbox-Timestamp": timestamp,
            "X-Sandbox-Nonce": nonce,
            "X-Sandbox-Signature": signature,
        }
        fd = os.open(STATE, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as output:
            json.dump(headers, output)
        code = request(headers)
        if code != 400:
            raise SystemExit(f"first signed invalid JSON: expected 400, got {code}")
    else:
        headers = json.load(open(STATE))
        code = request(headers)
        if code != 401:
            raise SystemExit(f"replayed signed request: expected 401, got {code}")
    print(f"nonce smoke {sys.argv[1]} accepted expected status {code}")


if __name__ == "__main__":
    main()
