package agentruntime

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestRenderDOCXPreviewHTMLExtractsDocumentText(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create document.xml: %v", err)
	}
	if _, err := w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:t> DOCX</w:t></w:r></w:p>
    <w:p><w:r><w:t>Second paragraph</w:t></w:r></w:p>
  </w:body>
</w:document>`)); err != nil {
		t.Fatalf("write document.xml: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	html, err := renderDOCXPreviewHTML(&Artifact{Filename: "report.docx", ContentType: docxContentType}, buf.Bytes())
	if err != nil {
		t.Fatalf("render preview: %v", err)
	}
	body := string(html)
	for _, want := range []string{"report.docx", "Hello DOCX", "Second paragraph"} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview missing %q: %s", want, body)
		}
	}
}
