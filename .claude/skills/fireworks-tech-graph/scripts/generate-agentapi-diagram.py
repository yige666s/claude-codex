#!/usr/bin/env python3
"""
AgentAPI wrapper for Fireworks Tech Graph.

Reads a natural-language diagram request from stdin, converts it into the
JSON shape expected by generate-from-template.py, writes a self-contained SVG
under the current workspace, and prints an Artifact-tool contract.
"""

from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import re
import sys
import textwrap
import time
import urllib.error
import urllib.request
import xml.etree.ElementTree as ET
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple


SCRIPT_DIR = Path(__file__).resolve().parent
GENERATOR_PATH = SCRIPT_DIR / "generate-from-template.py"
SUPPORTED_TYPES = {
    "architecture",
    "data-flow",
    "flowchart",
    "sequence",
    "comparison",
    "timeline",
    "mind-map",
    "agent",
    "memory",
    "use-case",
    "class",
    "state-machine",
    "er-diagram",
    "network-topology",
}


def load_renderer():
    spec = importlib.util.spec_from_file_location("fireworks_template_renderer", GENERATOR_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError("diagram renderer is unavailable")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def clean_text(value: str, limit: int = 72) -> str:
    text = re.sub(r"\s+", " ", value).strip()
    if len(text) <= limit:
        return text
    return text[: limit - 1].rstrip() + "..."


def slugify(value: str, fallback: str = "diagram") -> str:
    text = value.lower()
    text = re.sub(r"https?://", "", text)
    text = re.sub(r"[^a-z0-9]+", "-", text)
    text = re.sub(r"-+", "-", text).strip("-")
    return (text or fallback)[:80].strip("-") or fallback


def infer_diagram_type(prompt: str) -> str:
    lower = prompt.lower()
    checks = [
        ("sequence", ["sequence", "时序", "调用链", "交互顺序"]),
        ("data-flow", ["data flow", "数据流", "流转", "pipeline", "管道"]),
        ("flowchart", ["flowchart", "流程图", "步骤", "决策"]),
        ("timeline", ["timeline", "时间线", "路线图", "roadmap"]),
        ("er-diagram", ["er diagram", "实体关系", "数据库关系", "schema"]),
        ("class", ["class diagram", "类图", "uml class"]),
        ("state-machine", ["state machine", "状态机"]),
        ("network-topology", ["network", "topology", "网络拓扑"]),
        ("mind-map", ["mind map", "脑图", "概念图"]),
    ]
    for diagram_type, needles in checks:
        if any(needle in lower for needle in needles):
            return diagram_type
    return "architecture"


def infer_style(prompt: str) -> int:
    match = re.search(r"(?:--style|style|风格)\s*[=:]?\s*([1-7])\b", prompt, flags=re.I)
    if match:
        return int(match.group(1))
    lower = prompt.lower()
    if "dark" in lower or "terminal" in lower or "深色" in lower:
        return 2
    if "blueprint" in lower or "蓝图" in lower:
        return 3
    if "notion" in lower or "clean" in lower or "简洁" in lower:
        return 4
    if "glass" in lower or "玻璃" in lower:
        return 5
    if "openai" in lower:
        return 7
    return 7


def github_repo(prompt: str) -> Optional[Tuple[str, str]]:
    match = re.search(r"github\.com[:/]+([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)", prompt)
    if not match:
        return None
    owner = match.group(1).strip(".")
    repo = match.group(2).removesuffix(".git").strip(".")
    if owner and repo:
        return owner, repo
    return None


def request_json(url: str, timeout: float = 6.0) -> Optional[Dict[str, Any]]:
    req = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "User-Agent": "AgentAPI-Fireworks-Tech-Graph",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            if response.status >= 400:
                return None
            return json.loads(response.read().decode("utf-8"))
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError):
        return None


def fetch_github_tree(owner: str, repo: str) -> Tuple[Dict[str, Any], List[str]]:
    meta = request_json(f"https://api.github.com/repos/{owner}/{repo}") or {}
    branch = str(meta.get("default_branch") or "main")
    tree_doc = request_json(f"https://api.github.com/repos/{owner}/{repo}/git/trees/{branch}?recursive=1") or {}
    paths = []
    for item in tree_doc.get("tree", [])[:1600]:
        path = item.get("path")
        if isinstance(path, str):
            paths.append(path)
    return meta, paths


def has_any(paths: Iterable[str], patterns: Iterable[str]) -> bool:
    joined = "\n".join(path.lower() for path in paths)
    return any(re.search(pattern, joined) for pattern in patterns)


def repo_components(owner: str, repo: str, meta: Dict[str, Any], paths: List[str]) -> List[Dict[str, str]]:
    components: List[Dict[str, str]] = [
        {"id": "clients", "label": "Clients", "type": "USERS", "kind": "rect", "layer": "entry"},
        {"id": "api", "label": "Public API", "type": "INTERFACE", "kind": "double_rect", "layer": "entry"},
        {"id": "core", "label": f"{repo} Core", "type": "RUNTIME", "kind": "double_rect", "layer": "core"},
    ]

    if has_any(paths, [r"(^|/)server\.", r"(^|/)cmd/", r"(^|/)src/.*server", r"daemon", r"network", r"protocol"]):
        components.append({"id": "server", "label": "Server / Protocol", "type": "IO", "kind": "rect", "layer": "core"})
    if has_any(paths, [r"storage", r"persist", r"snapshot", r"wal", r"aof", r"rdb", r"db\.", r"database"]):
        components.append({"id": "storage", "label": "Persistence", "type": "STATE", "kind": "cylinder", "layer": "data"})
    if has_any(paths, [r"cache", r"memory", r"store", r"dict", r"index", r"object"]):
        components.append({"id": "memory", "label": "In-Memory Store", "type": "DATA", "kind": "cylinder", "layer": "data"})
    if has_any(paths, [r"cluster", r"shard", r"replica", r"replication", r"sentinel", r"raft", r"failover"]):
        components.append({"id": "ha", "label": "Replication / Cluster", "type": "HA", "kind": "circle_cluster", "layer": "coord"})
    if has_any(paths, [r"module", r"plugin", r"extension", r"addon"]):
        components.append({"id": "modules", "label": "Modules", "type": "EXTEND", "kind": "rect", "layer": "coord"})
    if has_any(paths, [r"queue", r"stream", r"event", r"pubsub", r"subscribe"]):
        components.append({"id": "events", "label": "Streams / Events", "type": "ASYNC", "kind": "rect", "layer": "coord"})
    if has_any(paths, [r"test", r"bench", r"metrics", r"trace", r"monitor", r"logging"]):
        components.append({"id": "ops", "label": "Tests / Observability", "type": "OPS", "kind": "rect", "layer": "ops"})

    seen = set()
    deduped = []
    for item in components:
        if item["id"] in seen:
            continue
        seen.add(item["id"])
        deduped.append(item)
    return deduped


def prompt_components(prompt: str) -> List[Dict[str, str]]:
    lower = prompt.lower()
    base = [
        {"id": "users", "label": "Users / Clients", "type": "ENTRY", "kind": "rect", "layer": "entry"},
        {"id": "gateway", "label": "Gateway / API", "type": "EDGE", "kind": "double_rect", "layer": "entry"},
        {"id": "runtime", "label": "Core Runtime", "type": "SERVICE", "kind": "double_rect", "layer": "core"},
    ]
    optional = [
        ("auth", ["auth", "login", "权限", "认证"], {"id": "auth", "label": "Auth / Policy", "type": "GUARD", "kind": "rect", "layer": "core"}),
        ("worker", ["worker", "job", "queue", "任务", "队列"], {"id": "worker", "label": "Workers / Jobs", "type": "ASYNC", "kind": "rect", "layer": "coord"}),
        ("cache", ["cache", "redis", "缓存"], {"id": "cache", "label": "Cache", "type": "FAST", "kind": "cylinder", "layer": "data"}),
        ("db", ["database", "postgres", "mysql", "数据库", "存储"], {"id": "db", "label": "Database", "type": "STATE", "kind": "cylinder", "layer": "data"}),
        ("object", ["s3", "r2", "object", "对象存储", "artifact"], {"id": "object", "label": "Object Storage", "type": "FILES", "kind": "cylinder", "layer": "data"}),
        ("llm", ["llm", "model", "模型", "agent"], {"id": "model", "label": "Model / Agent", "type": "AI", "kind": "bot", "layer": "core"}),
        ("ops", ["monitor", "log", "metrics", "监控", "日志"], {"id": "ops", "label": "Observability", "type": "OPS", "kind": "rect", "layer": "ops"}),
    ]
    items = list(base)
    for _, needles, component in optional:
        if any(needle in lower for needle in needles):
            items.append(component)
    if len(items) < 6:
        items.extend(
            [
                {"id": "data", "label": "Data Store", "type": "STATE", "kind": "cylinder", "layer": "data"},
                {"id": "ops", "label": "Observability", "type": "OPS", "kind": "rect", "layer": "ops"},
            ]
        )
    return items


def title_from_prompt(prompt: str, repo: Optional[Tuple[str, str]]) -> str:
    if repo:
        return f"{repo[1].replace('-', ' ').replace('_', ' ').title()} Architecture"
    text = re.sub(r"https?://\S+", "", prompt)
    text = re.sub(r"--\w+(?:[= ]\S+)?", "", text).strip(" ：:，,。.")
    text = re.sub(r"^(帮我|请|画|生成|创建|做个|draw|generate|create)\s*", "", text, flags=re.I)
    if not text:
        return "Technical Architecture"
    first = re.split(r"[。.!?？；;\n]", text, maxsplit=1)[0]
    return clean_text(first, 44) or "Technical Architecture"


def component_layout(components: List[Dict[str, str]]) -> Tuple[List[Dict[str, Any]], List[Dict[str, Any]]]:
    layers = {
        "entry": {"y": 128, "items": []},
        "core": {"y": 288, "items": []},
        "coord": {"y": 448, "items": []},
        "data": {"y": 448, "items": []},
        "ops": {"y": 606, "items": []},
    }
    for component in components:
        layers.setdefault(component.get("layer", "core"), {"y": 288, "items": []})["items"].append(component)

    nodes: List[Dict[str, Any]] = []
    for layer, spec in layers.items():
        items = spec["items"]
        if not items:
            continue
        width = 168
        gap = 36
        total = len(items) * width + (len(items) - 1) * gap
        start = max(72, (1120 - total) / 2)
        for idx, item in enumerate(items):
            nodes.append(
                {
                    "id": item["id"],
                    "kind": item.get("kind", "rect"),
                    "x": round(start + idx * (width + gap), 2),
                    "y": spec["y"],
                    "width": width,
                    "height": 68 if item.get("kind") != "cylinder" else 92,
                    "label": clean_text(item["label"], 24),
                    "type_label": item.get("type", ""),
                    "flat": item.get("kind") == "rect",
                }
            )

    arrows: List[Dict[str, Any]] = []
    ids = {node["id"] for node in nodes}
    def add(source: str, target: str, flow: str = "control", label: str = "") -> None:
        if source in ids and target in ids:
            arrow: Dict[str, Any] = {
                "source": source,
                "target": target,
                "source_port": "bottom" if source != "clients" and source != "users" else "right",
                "target_port": "top" if target not in {"api", "gateway"} else "left",
                "flow": flow,
            }
            if label:
                arrow["label"] = label
            arrows.append(arrow)

    entry = "api" if "api" in ids else "gateway"
    client = "clients" if "clients" in ids else "users"
    core = "core" if "core" in ids else "runtime"
    add(client, entry, "read", "request")
    add(entry, core, "control", "dispatch")
    for target, flow, label in [
        ("server", "read", "protocol"),
        ("auth", "control", "authorize"),
        ("model", "control", "reason"),
        ("worker", "async", "enqueue"),
        ("events", "async", "publish"),
        ("memory", "data", "read/write"),
        ("cache", "data", "cache"),
        ("db", "write", "persist"),
        ("storage", "write", "persist"),
        ("object", "write", "files"),
        ("ha", "feedback", "sync"),
        ("modules", "control", "extend"),
        ("ops", "feedback", "observe"),
    ]:
        source = "server" if target in {"memory", "storage", "cache", "db", "ha", "modules", "events"} and "server" in ids else core
        add(source, target, flow, label)
    if "ha" in ids and "storage" in ids:
        add("ha", "storage", "feedback", "replicate")
    return nodes, arrows


def build_data(prompt: str) -> Tuple[str, str, Dict[str, Any]]:
    repo = github_repo(prompt)
    meta: Dict[str, Any] = {}
    paths: List[str] = []
    if repo:
        meta, paths = fetch_github_tree(*repo)
        components = repo_components(repo[0], repo[1], meta, paths)
        subtitle = clean_text(str(meta.get("description") or f"Public GitHub repository: {repo[0]}/{repo[1]}"), 120)
    else:
        components = prompt_components(prompt)
        subtitle = clean_text(prompt, 120)

    nodes, arrows = component_layout(components)
    data = {
        "template_type": infer_diagram_type(prompt),
        "style": infer_style(prompt),
        "width": 1120,
        "height": 760,
        "title": title_from_prompt(prompt, repo),
        "subtitle": subtitle,
        "containers": [
            {"x": 40, "y": 100, "width": 1040, "height": 118, "label": "ENTRY", "header_prefix": "01"},
            {"x": 40, "y": 260, "width": 1040, "height": 126, "label": "CORE", "header_prefix": "02"},
            {"x": 40, "y": 420, "width": 1040, "height": 128, "label": "COORDINATION + DATA", "header_prefix": "03"},
            {"x": 40, "y": 580, "width": 1040, "height": 112, "label": "OPERATIONS", "header_prefix": "04"},
        ],
        "nodes": nodes,
        "arrows": arrows,
        "legend": [
            {"flow": "read", "label": "User/request path"},
            {"flow": "control", "label": "Control flow"},
            {"flow": "write", "label": "Persistence"},
            {"flow": "feedback", "label": "Sync/feedback"},
        ],
        "legend_position": "bottom-right",
        "legend_y": 706,
        "footer": "Generated from the supplied prompt and public repository metadata when available.",
    }
    template_type = data["template_type"]
    if template_type not in SUPPORTED_TYPES:
        template_type = "architecture"
    return template_type, data["title"], data


def validate_svg(svg: str) -> None:
    root = ET.fromstring(svg)
    if not root.tag.endswith("svg"):
        raise ValueError("generated document is not SVG")


def workspace_dir() -> Path:
    configured = os.environ.get("AGENT_WORKSPACE_DIR", "").strip()
    if configured:
        return Path(configured)
    return Path.cwd()


def main() -> int:
    prompt = sys.stdin.read().strip()
    if not prompt:
        print("skill_error: 请提供要绘制的架构、流程或系统描述。")
        return 0

    prompt_hash = hashlib.sha256(prompt.encode("utf-8")).hexdigest()[:16]
    started = time.time()
    try:
        renderer = load_renderer()
        template_type, title, data = build_data(prompt)
        svg = renderer.build_svg(template_type, data)
        validate_svg(svg)

        output_dir = workspace_dir() / "generated-artifacts"
        output_dir.mkdir(parents=True, exist_ok=True)
        filename = f"{slugify(title, 'tech-graph')}-{prompt_hash[:8]}.svg"
        output_path = output_dir / filename
        output_path.write_text(svg, encoding="utf-8")
        relative_path = Path("generated-artifacts") / filename

        duration_ms = int((time.time() - started) * 1000)
        log = {
            "ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "skill": "fireworks-tech-graph",
            "event": "success",
            "prompt_hash": prompt_hash,
            "template_type": template_type,
            "filename": filename,
            "content_type": "image/svg+xml",
            "size_bytes": output_path.stat().st_size,
            "duration_ms": duration_ms,
        }
        print("skill_log: " + json.dumps(log, ensure_ascii=False, sort_keys=True))
        print(f"output_file: {output_path.resolve()}")
        print(f"artifact_file_path: {relative_path.as_posix()}")
        print(f"filename: {filename}")
        print("content_type: image/svg+xml")
        print(f"template_type: {template_type}")
        return 0
    except Exception as exc:
        message = "图表生成失败，请稍后再试，或把系统组成、关键节点和连接关系描述得更具体。"
        print("skill_error: " + message)
        print("error_kind: diagram_generation_failed")
        detail = textwrap.shorten(str(exc), width=180, placeholder="...")
        log = {
            "ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "skill": "fireworks-tech-graph",
            "event": "error",
            "prompt_hash": prompt_hash,
            "kind": "diagram_generation_failed",
            "detail": detail,
        }
        print("skill_log: " + json.dumps(log, ensure_ascii=False, sort_keys=True))
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
