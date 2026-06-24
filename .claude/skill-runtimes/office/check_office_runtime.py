#!/usr/bin/env python3
"""Smoke-check runtime dependencies for imported office skills."""

from __future__ import annotations

import importlib
import json
import os
import shutil
import subprocess
import sys


PYTHON_MODULES = [
    "docx",
    "lxml",
    "openpyxl",
    "pdf2image",
    "pdfplumber",
    "PIL",
    "pptx",
    "pypdf",
    "reportlab",
]

BINARIES = ["python3", "soffice", "pdftoppm", "pdfinfo", "node"]

NODE_PACKAGES = ["docx", "exceljs", "pptxgenjs"]


def check_python_modules() -> dict[str, str]:
    result: dict[str, str] = {}
    for name in PYTHON_MODULES:
        try:
            importlib.import_module(name)
        except Exception as exc:  # pragma: no cover - diagnostic script
            result[name] = f"missing: {exc}"
        else:
            result[name] = "ok"
    return result


def check_binaries() -> dict[str, str]:
    return {name: shutil.which(name) or "missing" for name in BINARIES}


def check_node_packages() -> dict[str, str]:
    env = os.environ.copy()
    node_path = env.get("OFFICE_RUNTIME_NODE_MODULES") or env.get("NODE_PATH")
    if node_path:
        env["NODE_PATH"] = node_path
    result: dict[str, str] = {}
    for pkg in NODE_PACKAGES:
        proc = subprocess.run(
            ["node", "-e", f"require.resolve({pkg!r})"],
            env=env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        if proc.returncode == 0:
            result[pkg] = proc.stdout.strip() or "ok"
        else:
            result[pkg] = "missing"
    return result


def main() -> int:
    report = {
        "python": sys.executable,
        "python_modules": check_python_modules(),
        "binaries": check_binaries(),
        "node_path": os.environ.get("NODE_PATH", ""),
        "office_runtime_node_modules": os.environ.get("OFFICE_RUNTIME_NODE_MODULES", ""),
        "node_packages": check_node_packages(),
    }
    print(json.dumps(report, ensure_ascii=False, indent=2))
    flattened = [
        *report["python_modules"].values(),
        *report["binaries"].values(),
        *report["node_packages"].values(),
    ]
    return 1 if any(value == "missing" or value.startswith("missing:") for value in flattened) else 0


if __name__ == "__main__":
    raise SystemExit(main())
