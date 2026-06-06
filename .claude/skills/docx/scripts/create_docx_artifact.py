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
    if looks_like_unresolved_reference(text):
        print("skill_error: 当前输入只包含对上下文的引用，没有包含实际文档正文。请先根据对话上下文整理出完整正文，再调用此脚本生成 docx。")
        return 1
    if looks_like_generation_brief(text):
        print("skill_error: requires_final_body: 当前输入看起来是文档生成需求或格式要求，不是最终文档正文。请先撰写完整正文，只把最终正文传给 docx 生成脚本。")
        return 1

    workspace = Path(os.environ.get("AGENT_WORKSPACE_DIR") or os.getcwd())
    output_dir = workspace / "generated-artifacts"
    output_dir.mkdir(parents=True, exist_ok=True)

    title, body_text = extract_title(text)
    filename = unique_filename(output_dir, safe_filename(title) + ".docx")
    output_path = output_dir / filename
    write_docx(output_path, title, split_paragraphs(body_text))

    print(f"output_file: {output_path}")
    print(f"artifact_file_path: generated-artifacts/{filename}")
    print(f"filename: {filename}")
    print(f"content_type: {DOCX_CONTENT_TYPE}")
    return 0


def normalize_input(raw: str) -> str:
    text = raw.replace("\r\n", "\n").replace("\r", "\n").strip()
    text = re.sub(r"^\s*/docx\b", "", text, flags=re.IGNORECASE).strip()
    return text


def looks_like_unresolved_reference(text: str) -> bool:
    compact = re.sub(r"\s+", "", text)
    if len(compact) > 80:
        return False
    reference_words = ("上边", "上面", "上述", "以上", "前面", "previous", "above")
    document_words = ("docx", "word", "文档", "文件")
    return any(word in compact.lower() for word in reference_words) and any(
        word in compact.lower() for word in document_words
    )


def looks_like_generation_brief(text: str) -> bool:
    compact = re.sub(r"\s+", "", text).lower()
    if len(compact) < 40:
        return False
    strong_markers = (
        "请直接使用docx",
        "请使用docx",
        "使用docx技能",
        "调用docx技能",
        "报告生成要求",
        "文档生成要求",
        "生成要求",
        "报告生成要求：",
        "生成该文档",
        "生成此文档",
    )
    if any(marker in compact for marker in strong_markers):
        return True
    request_markers = (
        "请为我生成",
        "请帮我生成",
        "帮我生成",
        "生成一份",
        "撰写一份",
        "输出一份",
        "请基于以下",
    )
    requirement_markers = (
        "结构规范",
        "格式排版",
        "语言：",
        "语言:",
        "包含封面",
        "包含目录",
        "章节分明",
        "报告要求",
        "文档要求",
    )
    request_hits = sum(1 for marker in request_markers if marker in compact)
    requirement_hits = sum(1 for marker in requirement_markers if marker in compact)
    return request_hits > 0 and requirement_hits > 0


def extract_title(text: str) -> tuple[str, str]:
    lines = [line.rstrip() for line in text.splitlines()]
    for index, line in enumerate(lines):
        stripped = line.strip()
        if not stripped:
            continue
        markdown_heading = re.match(r"^#{1,2}\s+(.+)$", stripped)
        if markdown_heading:
            title = clean_title(markdown_heading.group(1))
            body = "\n".join(lines[:index] + lines[index + 1 :]).strip()
            return title, body or title
        if len(stripped) <= 60 and not sentence_like(stripped):
            title = clean_title(stripped)
            body = "\n".join(lines[:index] + lines[index + 1 :]).strip()
            return title, body or title
        break
    return "Word文档", text


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
        text = re.sub(r"([。！？!?])\s+", r"\1\n", text)
        text = re.sub(r"([;；])\s+", r"\1\n", text)
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
    return len(compact) <= 60 and (compact.endswith("：") or compact.endswith(":"))


def sentence_like(text: str) -> bool:
    return bool(re.search(r"[。.!?？；;，,]", text))


def clean_title(text: str) -> str:
    title = text.strip(" #\t:")
    title = re.sub(r"\s+", " ", title)
    return title[:60] or "Word文档"


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
