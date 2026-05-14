#!/usr/bin/env python3
"""Create a small, valid DOCX artifact from stdin.

This script intentionally uses only the Python standard library so the docx
skill can create a reliable artifact in the runtime sandbox without depending
on npm or LibreOffice for the common "turn this text into a Word document" path.
"""

from __future__ import annotations

import os
import re
import sys
import time
import zipfile
from pathlib import Path
from xml.sax.saxutils import escape


DOCX_CONTENT_TYPE = (
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)


def main() -> int:
    raw = sys.stdin.read()
    text = normalize_input(raw)
    if not text:
        print("skill_error: 没有收到可写入 Word 文档的内容。请提供要整理成 docx 的文本或上传文本附件。")
        return 1

    workspace = Path(os.environ.get("AGENT_WORKSPACE_DIR") or os.getcwd())
    output_dir = workspace / "generated-artifacts"
    output_dir.mkdir(parents=True, exist_ok=True)

    title = infer_title(text)
    filename = unique_filename(output_dir, safe_filename(title) + ".docx")
    output_path = output_dir / filename
    write_docx(output_path, title, split_paragraphs(text))

    print(f"output_file: {output_path}")
    print(f"artifact_file_path: generated-artifacts/{filename}")
    print(f"filename: {filename}")
    print(f"content_type: {DOCX_CONTENT_TYPE}")
    return 0


def normalize_input(raw: str) -> str:
    text = raw.replace("\r\n", "\n").replace("\r", "\n").strip()
    text = re.sub(r"^\s*/docx\b", "", text, flags=re.IGNORECASE).strip()
    text = re.sub(r"^把这份总结生成一个\s*Docx\s*文档\s*$", "", text, flags=re.IGNORECASE).strip()
    return text


def infer_title(text: str) -> str:
    for line in text.splitlines():
        line = line.strip(" #\t:")
        if line:
            candidate = re.split(r"[。.!?？；;]", line, maxsplit=1)[0].strip()
            if 4 <= len(candidate) <= 48:
                return candidate
            break
    if "AI 女友" in text or "AI女友" in text:
        return "AI_女友产品方案总结"
    return "生成文档"


def safe_filename(title: str) -> str:
    title = title.strip() or "generated-document"
    title = re.sub(r"[\\/:*?\"<>|]+", "_", title)
    title = re.sub(r"\s+", "_", title)
    title = title.strip("._")
    if not title:
        title = "generated-document"
    return title[:80]


def unique_filename(output_dir: Path, filename: str) -> str:
    stem = Path(filename).stem
    suffix = Path(filename).suffix or ".docx"
    candidate = stem + suffix
    if not (output_dir / candidate).exists():
        return candidate
    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    return f"{stem}-{stamp}{suffix}"


def split_paragraphs(text: str) -> list[str]:
    text = text.strip()
    if "\n" not in text:
        headings = [
            "OpenClaw 云端沙盒方案对比",
            "推荐路径",
            "长时间运行 agent 的关键设计",
            "AI 女友产品无状态改造方案评估",
            "OpenClaw 改造路径",
            "Claw 生态轻量方案对比",
            "MicroClaw vs ZeptoClaw 深度对比",
            "最终架构建议",
            "关于无状态模型与 ZeptoClaw agent 的关系",
            "然而，当前的 ZeptoClaw 实现仍有待改进",
        ]
        for heading in headings:
            text = text.replace(f" {heading}", f"\n\n{heading}")
        text = re.sub(r" (?=(Firecracker|gVisor|Nydus|托管服务|早期验证|规模增长|大规模降本|状态持久化|心跳|分层存储|可行性|核心思路|关键问题与解决|记忆加载延迟|记忆存储分层设计|技能动态加载|并发问题|改造前后对比|具体改造点|改造工作量|建议|推荐 ZeptoClaw|ZeroClaw|ZeptoClaw 优势|MicroClaw 优势|结论|容器启动开销|记忆外部化|Session 外部化)[:：])", "\n", text)
    paragraphs = [line.strip() for line in re.split(r"\n{1,}", text) if line.strip()]
    return paragraphs or [text]


def write_docx(path: Path, title: str, paragraphs: list[str]) -> None:
    with zipfile.ZipFile(path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        archive.writestr("[Content_Types].xml", content_types_xml())
        archive.writestr("_rels/.rels", rels_xml())
        archive.writestr("word/_rels/document.xml.rels", document_rels_xml())
        archive.writestr("word/styles.xml", styles_xml())
        archive.writestr("word/document.xml", document_xml(title, paragraphs))


def content_types_xml() -> str:
    return """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
</Types>
"""


def rels_xml() -> str:
    return """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>
"""


def document_rels_xml() -> str:
    return """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>
"""


def styles_xml() -> str:
    return """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal">
    <w:name w:val="Normal"/>
    <w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:eastAsia="Microsoft YaHei"/><w:sz w:val="22"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Title">
    <w:name w:val="Title"/>
    <w:basedOn w:val="Normal"/>
    <w:pPr><w:spacing w:after="240"/></w:pPr>
    <w:rPr><w:b/><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:eastAsia="Microsoft YaHei"/><w:sz w:val="34"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading1">
    <w:name w:val="heading 1"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr><w:spacing w:before="280" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr>
    <w:rPr><w:b/><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:eastAsia="Microsoft YaHei"/><w:sz w:val="28"/></w:rPr>
  </w:style>
</w:styles>
"""


def document_xml(title: str, paragraphs: list[str]) -> str:
    body: list[str] = [paragraph(title, style="Title")]
    for item in paragraphs:
        style = "Heading1" if looks_like_heading(item) else None
        body.append(paragraph(item, style=style))
    body.append("""<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>""")
    return f"""<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    {''.join(body)}
  </w:body>
</w:document>
"""


def looks_like_heading(text: str) -> bool:
    compact = text.strip()
    return len(compact) <= 40 and (compact.endswith("：") or compact.endswith(":") or "方案" in compact or "路径" in compact or "建议" in compact)


def paragraph(text: str, style: str | None = None) -> str:
    style_xml = f'<w:pPr><w:pStyle w:val="{style}"/></w:pPr>' if style else ""
    lines = escape(text).split("\n")
    runs = []
    for idx, line in enumerate(lines):
        if idx:
            runs.append("<w:br/>")
        runs.append(f"<w:t xml:space=\"preserve\">{line}</w:t>")
    return f"<w:p>{style_xml}<w:r>{''.join(runs)}</w:r></w:p>"


if __name__ == "__main__":
    raise SystemExit(main())
