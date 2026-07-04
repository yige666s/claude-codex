package agentruntime

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestRenderDOCXPreviewHTMLExtractsDocumentTextAndTables(t *testing.T) {
	data := zipFixture(t, map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:t> DOCX</w:t></w:r></w:p>
    <w:p><w:r><w:t>Second paragraph</w:t></w:r></w:p>
    <w:tbl>
      <w:tr><w:tc><w:p><w:r><w:t>Metric</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Value</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>Revenue</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>120</w:t></w:r></w:p></w:tc></w:tr>
    </w:tbl>
  </w:body>
</w:document>`,
	})

	html, err := renderDOCXPreviewHTML(&Artifact{Filename: "report.docx", ContentType: docxContentType}, data)
	if err != nil {
		t.Fatalf("render preview: %v", err)
	}
	body := string(html)
	for _, want := range []string{"report.docx", "Hello DOCX", "Second paragraph", "<td>Metric</td>", "<td>Revenue</td>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview missing %q: %s", want, body)
		}
	}
	for _, want := range []string{"body{margin:0;padding:16px", "box-sizing:border-box", "table{border-collapse:collapse"} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview style missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "min-height:calc(100vh - 32px)") {
		t.Fatalf("preview should size to document content instead of forcing viewport height: %s", body)
	}
}

func TestRenderXLSXPreviewHTMLExtractsSheetsAndSharedStrings(t *testing.T) {
	data := zipFixture(t, map[string]string{
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="Sales" sheetId="1" r:id="rId1"/></sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Target="worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/sharedStrings.xml": `<?xml version="1.0" encoding="UTF-8"?>
<sst><si><t>Month</t></si><si><t>Revenue</t></si><si><t>January</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet><sheetData>
  <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
  <row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>120</v></c></row>
</sheetData></worksheet>`,
	})

	html, err := renderOfficePreviewHTML(&Artifact{Filename: "sales.xlsx", ContentType: xlsxContentType}, data)
	if err != nil {
		t.Fatalf("render xlsx preview: %v", err)
	}
	body := string(html)
	for _, want := range []string{"XLSX preview", "Sales", "<td>Month</td>", "<td>Revenue</td>", "<td>January</td>", "<td>120</td>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview missing %q: %s", want, body)
		}
	}
}

func TestRenderPPTXPreviewHTMLExtractsSlideText(t *testing.T) {
	data := zipFixture(t, map[string]string{
		"ppt/slides/slide2.xml": `<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld><p:spTree><p:sp><p:txBody>
    <a:p><a:r><a:t>Second Slide</a:t></a:r></a:p>
    <a:p><a:r><a:t>Later content</a:t></a:r></a:p>
  </p:txBody></p:sp></p:spTree></p:cSld>
</p:sld>`,
		"ppt/slides/slide1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld><p:spTree><p:sp><p:txBody>
    <a:p><a:r><a:t>AI Industry Impact</a:t></a:r></a:p>
    <a:p><a:r><a:t>Healthcare, finance, education</a:t></a:r></a:p>
  </p:txBody></p:sp></p:spTree></p:cSld>
</p:sld>`,
	})

	html, err := renderOfficePreviewHTML(&Artifact{Filename: "deck.pptx", ContentType: pptxContentType}, data)
	if err != nil {
		t.Fatalf("render pptx preview: %v", err)
	}
	body := string(html)
	for _, want := range []string{"PPTX preview", "Slide 1", "AI Industry Impact", "Healthcare, finance, education", "Slide 2", "Second Slide"} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview missing %q: %s", want, body)
		}
	}
	if strings.Index(body, "AI Industry Impact") > strings.Index(body, "Second Slide") {
		t.Fatalf("slides should render in numeric order: %s", body)
	}
}

func zipFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}
