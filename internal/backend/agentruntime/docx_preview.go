package agentruntime

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"strings"
)

const (
	docxContentType        = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	docxPreviewMaxTextSize = 200_000
)

func isDOCXAsset(asset *Artifact) bool {
	if asset == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(asset.ContentType), docxContentType) || strings.EqualFold(strings.TrimSpace(filepath.Ext(asset.Filename)), ".docx")
}

func renderDOCXPreviewHTML(asset *Artifact, data []byte) ([]byte, error) {
	if asset == nil {
		return nil, fmt.Errorf("artifact is required")
	}
	paragraphs, truncated, err := extractDOCXParagraphs(data, docxPreviewMaxTextSize)
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	out.WriteString(`<!doctype html><html><head><meta charset="utf-8"><style>`)
	out.WriteString(`:root{color-scheme:light;}body{margin:0;background:#f6f8fb;color:#17202a;font:15px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;}`)
	out.WriteString(`main{max-width:820px;margin:0 auto;padding:32px 36px 48px;background:#fff;min-height:100vh;box-shadow:0 0 0 1px #d8e0e8;}`)
	out.WriteString(`header{padding-bottom:16px;margin-bottom:20px;border-bottom:1px solid #d8e0e8;}h1{margin:0;font-size:18px;line-height:1.35;color:#111827;}small{display:block;margin-top:6px;color:#687586;}`)
	out.WriteString(`p{margin:0 0 12px;white-space:pre-wrap;overflow-wrap:anywhere;}.empty{min-height:1em}.notice{padding:10px 12px;margin-bottom:16px;border:1px solid #f3d19e;border-radius:8px;background:#fff8eb;color:#8a5a13;}`)
	out.WriteString(`</style></head><body><main><header><h1>`)
	out.WriteString(html.EscapeString(asset.Filename))
	out.WriteString(`</h1><small>DOCX preview</small></header>`)
	if truncated {
		out.WriteString(`<div class="notice">Preview truncated because the document is large. Download the file to view the full document.</div>`)
	}
	if len(paragraphs) == 0 {
		out.WriteString(`<p class="empty">No previewable text was found in this DOCX file.</p>`)
	}
	for _, paragraph := range paragraphs {
		if strings.TrimSpace(paragraph) == "" {
			out.WriteString(`<p class="empty">&nbsp;</p>`)
			continue
		}
		out.WriteString(`<p>`)
		out.WriteString(html.EscapeString(paragraph))
		out.WriteString(`</p>`)
	}
	out.WriteString(`</main></body></html>`)
	return []byte(out.String()), nil
}

func extractDOCXParagraphs(data []byte, maxTextSize int) ([]string, bool, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, false, fmt.Errorf("read docx zip: %w", err)
	}
	var document *zip.File
	for _, file := range reader.File {
		if file.Name == "word/document.xml" {
			document = file
			break
		}
	}
	if document == nil {
		return nil, false, fmt.Errorf("docx document.xml not found")
	}
	handle, err := document.Open()
	if err != nil {
		return nil, false, fmt.Errorf("open docx document.xml: %w", err)
	}
	defer handle.Close()

	decoder := xml.NewDecoder(handle)
	var paragraphs []string
	var paragraph strings.Builder
	inText := false
	textSize := 0
	truncated := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("parse docx document.xml: %w", err)
		}
		if truncated {
			continue
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "t":
				inText = true
			case "tab":
				_ = paragraph.WriteByte('\t')
				textSize++
			case "br", "cr":
				_ = paragraph.WriteByte('\n')
				textSize++
			}
		case xml.CharData:
			if !inText {
				continue
			}
			text := string(typed)
			if textSize+len(text) > maxTextSize {
				remaining := maxTextSize - textSize
				if remaining > 0 {
					paragraph.WriteString(text[:remaining])
				}
				truncated = true
				continue
			}
			textSize += len(text)
			paragraph.WriteString(text)
		case xml.EndElement:
			switch typed.Name.Local {
			case "t":
				inText = false
			case "p":
				paragraphs = append(paragraphs, paragraph.String())
				paragraph.Reset()
			}
		}
	}
	if paragraph.Len() > 0 {
		paragraphs = append(paragraphs, paragraph.String())
	}
	return paragraphs, truncated, nil
}
