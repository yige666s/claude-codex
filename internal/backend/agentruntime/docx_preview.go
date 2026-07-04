package agentruntime

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	docxContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	pptxContentType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"

	docxPreviewMaxTextSize = 200_000
	xlsxPreviewMaxSheets   = 8
	xlsxPreviewMaxRows     = 200
	xlsxPreviewMaxCols     = 40
	pptxPreviewMaxSlides   = 80
)

type officePreviewBlock struct {
	Kind string
	Text string
	Rows [][]string
}

type xlsxPreviewSheet struct {
	Name      string
	Rows      [][]string
	Truncated bool
}

type pptxPreviewSlide struct {
	Number int
	Title  string
	Lines  []string
}

type officeRelationship struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
}

type officeRelationships struct {
	Relationships []officeRelationship `xml:"Relationship"`
}

func isOfficePreviewAsset(asset *Artifact) bool {
	if asset == nil {
		return false
	}
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(asset.Filename)))
	contentType := strings.ToLower(strings.TrimSpace(asset.ContentType))
	return ext == ".docx" || ext == ".xlsx" || ext == ".pptx" ||
		contentType == docxContentType ||
		contentType == xlsxContentType ||
		contentType == pptxContentType
}

func isDOCXAsset(asset *Artifact) bool {
	if asset == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(asset.ContentType), docxContentType) || strings.EqualFold(strings.TrimSpace(filepath.Ext(asset.Filename)), ".docx")
}

func renderOfficePreviewHTML(asset *Artifact, data []byte) ([]byte, error) {
	if asset == nil {
		return nil, fmt.Errorf("artifact is required")
	}
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(asset.Filename)))
	contentType := strings.ToLower(strings.TrimSpace(asset.ContentType))
	switch {
	case ext == ".docx" || contentType == docxContentType:
		return renderDOCXPreviewHTML(asset, data)
	case ext == ".xlsx" || contentType == xlsxContentType:
		return renderXLSXPreviewHTML(asset, data)
	case ext == ".pptx" || contentType == pptxContentType:
		return renderPPTXPreviewHTML(asset, data)
	default:
		return nil, fmt.Errorf("preview is not available for this artifact type")
	}
}

func renderDOCXPreviewHTML(asset *Artifact, data []byte) ([]byte, error) {
	blocks, truncated, err := extractDOCXPreviewBlocks(data, docxPreviewMaxTextSize)
	if err != nil {
		return nil, err
	}
	var body strings.Builder
	if truncated {
		writeOfficeNotice(&body, "Preview truncated because the document is large. Download the file to view the full document.")
	}
	if len(blocks) == 0 {
		body.WriteString(`<p class="empty">No previewable text was found in this DOCX file.</p>`)
	}
	for _, block := range blocks {
		switch block.Kind {
		case "table":
			writeOfficeTable(&body, block.Rows)
		default:
			writeOfficeParagraph(&body, block.Text)
		}
	}
	return officePreviewHTML(asset.Filename, "DOCX preview", "document", body.String()), nil
}

func renderXLSXPreviewHTML(asset *Artifact, data []byte) ([]byte, error) {
	sheets, err := extractXLSXPreviewSheets(data)
	if err != nil {
		return nil, err
	}
	var body strings.Builder
	if len(sheets) == 0 {
		body.WriteString(`<p class="empty">No previewable sheets were found in this XLSX file.</p>`)
	}
	for _, sheet := range sheets {
		body.WriteString(`<section class="sheet"><h2>`)
		body.WriteString(html.EscapeString(sheet.Name))
		body.WriteString(`</h2>`)
		if sheet.Truncated {
			writeOfficeNotice(&body, fmt.Sprintf("Showing first %d rows and %d columns for this sheet.", xlsxPreviewMaxRows, xlsxPreviewMaxCols))
		}
		if len(sheet.Rows) == 0 {
			body.WriteString(`<p class="empty">This sheet is empty.</p>`)
		} else {
			writeOfficeTable(&body, sheet.Rows)
		}
		body.WriteString(`</section>`)
	}
	return officePreviewHTML(asset.Filename, "XLSX preview", "workbook", body.String()), nil
}

func renderPPTXPreviewHTML(asset *Artifact, data []byte) ([]byte, error) {
	slides, truncated, err := extractPPTXPreviewSlides(data)
	if err != nil {
		return nil, err
	}
	var body strings.Builder
	if truncated {
		writeOfficeNotice(&body, fmt.Sprintf("Showing first %d slides. Download the file to view the complete deck.", pptxPreviewMaxSlides))
	}
	if len(slides) == 0 {
		body.WriteString(`<p class="empty">No previewable slide text was found in this PPTX file.</p>`)
	}
	body.WriteString(`<div class="slide-grid">`)
	for _, slide := range slides {
		body.WriteString(`<article class="slide"><div class="slide-canvas"><small>Slide `)
		body.WriteString(strconv.Itoa(slide.Number))
		body.WriteString(`</small>`)
		if strings.TrimSpace(slide.Title) != "" {
			body.WriteString(`<h2>`)
			body.WriteString(html.EscapeString(slide.Title))
			body.WriteString(`</h2>`)
		}
		if len(slide.Lines) == 0 {
			body.WriteString(`<p class="empty">No text on this slide.</p>`)
		}
		for _, line := range slide.Lines {
			if strings.TrimSpace(line) == "" || line == slide.Title {
				continue
			}
			body.WriteString(`<p>`)
			body.WriteString(html.EscapeString(line))
			body.WriteString(`</p>`)
		}
		body.WriteString(`</div></article>`)
	}
	body.WriteString(`</div>`)
	return officePreviewHTML(asset.Filename, "PPTX preview", "deck", body.String()), nil
}

func extractDOCXParagraphs(data []byte, maxTextSize int) ([]string, bool, error) {
	blocks, truncated, err := extractDOCXPreviewBlocks(data, maxTextSize)
	if err != nil {
		return nil, false, err
	}
	var paragraphs []string
	for _, block := range blocks {
		if block.Kind == "paragraph" {
			paragraphs = append(paragraphs, block.Text)
		}
	}
	return paragraphs, truncated, nil
}

func extractDOCXPreviewBlocks(data []byte, maxTextSize int) ([]officePreviewBlock, bool, error) {
	files, err := openZipFileMap(data)
	if err != nil {
		return nil, false, fmt.Errorf("read docx zip: %w", err)
	}
	document, ok := files["word/document.xml"]
	if !ok {
		return nil, false, fmt.Errorf("docx document.xml not found")
	}
	decoder := xml.NewDecoder(bytes.NewReader(document))
	var blocks []officePreviewBlock
	var paragraph strings.Builder
	var cell strings.Builder
	var row []string
	var tableRows [][]string
	inText := false
	inTable := false
	inCell := false
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
			case "tbl":
				inTable = true
				tableRows = nil
			case "tr":
				if inTable {
					row = nil
				}
			case "tc":
				if inTable {
					inCell = true
					cell.Reset()
				}
			case "p":
				if !inTable {
					paragraph.Reset()
				}
			case "t":
				inText = true
			case "tab":
				textSize = appendDOCXText(&paragraph, &cell, inCell, "\t", textSize)
			case "br", "cr":
				textSize = appendDOCXText(&paragraph, &cell, inCell, "\n", textSize)
			}
		case xml.CharData:
			if !inText {
				continue
			}
			text := string(typed)
			if textSize+len(text) > maxTextSize {
				remaining := maxTextSize - textSize
				if remaining > 0 {
					appendDOCXText(&paragraph, &cell, inCell, text[:remaining], textSize)
				}
				truncated = true
				continue
			}
			textSize = appendDOCXText(&paragraph, &cell, inCell, text, textSize)
		case xml.EndElement:
			switch typed.Name.Local {
			case "t":
				inText = false
			case "p":
				if inTable && inCell && strings.TrimSpace(cell.String()) != "" && !strings.HasSuffix(cell.String(), "\n") {
					cell.WriteByte('\n')
				}
				if !inTable {
					blocks = append(blocks, officePreviewBlock{Kind: "paragraph", Text: paragraph.String()})
					paragraph.Reset()
				}
			case "tc":
				if inTable {
					row = append(row, strings.TrimSpace(cell.String()))
					cell.Reset()
					inCell = false
				}
			case "tr":
				if inTable {
					tableRows = append(tableRows, row)
				}
			case "tbl":
				blocks = append(blocks, officePreviewBlock{Kind: "table", Rows: tableRows})
				tableRows = nil
				inTable = false
			}
		}
	}
	return blocks, truncated, nil
}

func appendDOCXText(paragraph, cell *strings.Builder, inCell bool, text string, currentSize int) int {
	if inCell {
		cell.WriteString(text)
	} else {
		paragraph.WriteString(text)
	}
	return currentSize + len(text)
}

func extractXLSXPreviewSheets(data []byte) ([]xlsxPreviewSheet, error) {
	files, err := openZipFileMap(data)
	if err != nil {
		return nil, fmt.Errorf("read xlsx zip: %w", err)
	}
	workbook, ok := files["xl/workbook.xml"]
	if !ok {
		return nil, fmt.Errorf("xlsx workbook.xml not found")
	}
	relationships := parseOfficeRelationships(files["xl/_rels/workbook.xml.rels"], "xl")
	sharedStrings := parseXLSXSharedStrings(files["xl/sharedStrings.xml"])
	refs, err := parseXLSXWorkbookSheets(workbook, relationships)
	if err != nil {
		return nil, err
	}
	if len(refs) > xlsxPreviewMaxSheets {
		refs = refs[:xlsxPreviewMaxSheets]
	}
	var sheets []xlsxPreviewSheet
	for _, ref := range refs {
		content, ok := files[ref.Path]
		if !ok {
			continue
		}
		rows, truncated, err := parseXLSXSheetRows(content, sharedStrings)
		if err != nil {
			return nil, fmt.Errorf("parse sheet %s: %w", ref.Name, err)
		}
		sheets = append(sheets, xlsxPreviewSheet{Name: ref.Name, Rows: rows, Truncated: truncated})
	}
	return sheets, nil
}

type xlsxSheetRef struct {
	Name string
	Path string
}

func parseXLSXWorkbookSheets(data []byte, relationships map[string]string) ([]xlsxSheetRef, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var refs []xlsxSheetRef
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse xlsx workbook: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "sheet" {
			continue
		}
		name := attrValue(start, "name")
		if name == "" {
			name = "Sheet " + strconv.Itoa(len(refs)+1)
		}
		relID := attrValue(start, "id")
		target := relationships[relID]
		if target == "" {
			continue
		}
		refs = append(refs, xlsxSheetRef{Name: name, Path: target})
	}
	return refs, nil
}

func parseXLSXSharedStrings(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var values []string
	var current strings.Builder
	inSI := false
	inText := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return values
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local == "si" {
				inSI = true
				current.Reset()
			}
			if inSI && typed.Name.Local == "t" {
				inText = true
			}
		case xml.CharData:
			if inSI && inText {
				current.WriteString(string(typed))
			}
		case xml.EndElement:
			if typed.Name.Local == "t" {
				inText = false
			}
			if typed.Name.Local == "si" {
				values = append(values, current.String())
				inSI = false
			}
		}
	}
	return values
}

func parseXLSXSheetRows(data []byte, sharedStrings []string) ([][]string, bool, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var rows [][]string
	var row []string
	var cellRef string
	var cellType string
	var cellValue strings.Builder
	var inlineValue strings.Builder
	inCell := false
	inValue := false
	inInlineText := false
	truncated := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "row":
				row = nil
			case "c":
				inCell = true
				cellRef = attrValue(typed, "r")
				cellType = attrValue(typed, "t")
				cellValue.Reset()
				inlineValue.Reset()
			case "v":
				if inCell {
					inValue = true
				}
			case "t":
				if inCell && cellType == "inlineStr" {
					inInlineText = true
				}
			}
		case xml.CharData:
			if inValue {
				cellValue.WriteString(string(typed))
			}
			if inInlineText {
				inlineValue.WriteString(string(typed))
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "v":
				inValue = false
			case "t":
				inInlineText = false
			case "c":
				value := resolveXLSXCellValue(cellType, cellValue.String(), inlineValue.String(), sharedStrings)
				col := xlsxColumnIndex(cellRef)
				if col < 0 {
					col = len(row)
				}
				if col < xlsxPreviewMaxCols {
					row = ensureStringSliceLen(row, col+1)
					row[col] = value
				} else {
					truncated = true
				}
				inCell = false
			case "row":
				if len(rows) < xlsxPreviewMaxRows {
					rows = append(rows, trimTrailingEmptyCells(row))
				} else {
					truncated = true
				}
			}
		}
	}
	return rows, truncated, nil
}

func resolveXLSXCellValue(cellType, rawValue, inlineValue string, sharedStrings []string) string {
	rawValue = strings.TrimSpace(rawValue)
	switch cellType {
	case "s":
		index, err := strconv.Atoi(rawValue)
		if err == nil && index >= 0 && index < len(sharedStrings) {
			return sharedStrings[index]
		}
	case "inlineStr":
		return inlineValue
	case "b":
		if rawValue == "1" {
			return "TRUE"
		}
		if rawValue == "0" {
			return "FALSE"
		}
	}
	return rawValue
}

func extractPPTXPreviewSlides(data []byte) ([]pptxPreviewSlide, bool, error) {
	files, err := openZipFileMap(data)
	if err != nil {
		return nil, false, fmt.Errorf("read pptx zip: %w", err)
	}
	paths := sortedPPTXSlidePaths(files)
	truncated := false
	if len(paths) > pptxPreviewMaxSlides {
		paths = paths[:pptxPreviewMaxSlides]
		truncated = true
	}
	var slides []pptxPreviewSlide
	for index, slidePath := range paths {
		lines, err := parsePPTXSlideText(files[slidePath])
		if err != nil {
			return nil, false, fmt.Errorf("parse slide %s: %w", slidePath, err)
		}
		title := ""
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				title = line
				break
			}
		}
		slides = append(slides, pptxPreviewSlide{Number: index + 1, Title: title, Lines: lines})
	}
	return slides, truncated, nil
}

func sortedPPTXSlidePaths(files map[string][]byte) []string {
	var paths []string
	for name := range files {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") {
			paths = append(paths, name)
		}
	}
	slideNumber := regexp.MustCompile(`slide(\d+)\.xml$`)
	sort.Slice(paths, func(i, j int) bool {
		left := pptxSlideNumber(slideNumber, paths[i])
		right := pptxSlideNumber(slideNumber, paths[j])
		if left == right {
			return paths[i] < paths[j]
		}
		return left < right
	})
	return paths
}

func pptxSlideNumber(re *regexp.Regexp, value string) int {
	matches := re.FindStringSubmatch(value)
	if len(matches) != 2 {
		return 0
	}
	number, _ := strconv.Atoi(matches[1])
	return number
}

func parsePPTXSlideText(data []byte) ([]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var lines []string
	var paragraph strings.Builder
	inParagraph := false
	inText := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "p":
				inParagraph = true
				paragraph.Reset()
			case "t":
				if inParagraph {
					inText = true
				}
			}
		case xml.CharData:
			if inText {
				paragraph.WriteString(string(typed))
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "t":
				inText = false
			case "p":
				if inParagraph {
					lines = append(lines, strings.TrimSpace(paragraph.String()))
				}
				inParagraph = false
			}
		}
	}
	return compactStrings(lines), nil
}

func openZipFileMap(data []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(handle)
		closeErr := handle.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		files[path.Clean(file.Name)] = content
	}
	return files, nil
}

func parseOfficeRelationships(data []byte, baseDir string) map[string]string {
	out := map[string]string{}
	if len(data) == 0 {
		return out
	}
	var rels officeRelationships
	if err := xml.Unmarshal(data, &rels); err != nil {
		return out
	}
	for _, rel := range rels.Relationships {
		target := strings.TrimSpace(rel.Target)
		if target == "" || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			continue
		}
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(target, "/")
		} else {
			target = path.Join(baseDir, target)
		}
		out[rel.ID] = path.Clean(target)
	}
	return out
}

func officePreviewHTML(filename, subtitle, kindClass, inner string) []byte {
	var out strings.Builder
	out.WriteString(`<!doctype html><html><head><meta charset="utf-8"><style>`)
	out.WriteString(`:root{color-scheme:light;}html{min-height:100%;background:#f6f8fb;}*{box-sizing:border-box;}body{margin:0;padding:16px;background:#f6f8fb;color:#17202a;font:15px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;}`)
	out.WriteString(`main{max-width:980px;margin:0 auto;padding:32px 36px 48px;background:#fff;box-shadow:0 0 0 1px #d8e0e8;}main.deck{max-width:1180px;background:#eef2f7;box-shadow:none;}`)
	out.WriteString(`header{padding-bottom:16px;margin-bottom:20px;border-bottom:1px solid #d8e0e8;}h1{margin:0;font-size:18px;line-height:1.35;color:#111827;}h2{margin:18px 0 10px;font-size:16px;line-height:1.35;color:#111827;}small{display:block;margin-top:6px;color:#687586;}`)
	out.WriteString(`p{margin:0 0 12px;white-space:pre-wrap;overflow-wrap:anywhere;}.empty{min-height:1em;color:#687586}.notice{padding:10px 12px;margin-bottom:16px;border:1px solid #f3d19e;border-radius:8px;background:#fff8eb;color:#8a5a13;}`)
	out.WriteString(`.table-wrap{width:100%;overflow:auto;margin:12px 0 20px;border:1px solid #d8e0e8;border-radius:8px;background:#fff;}table{border-collapse:collapse;min-width:100%;font-size:13px;}td,th{border:1px solid #d8e0e8;padding:7px 9px;text-align:left;vertical-align:top;white-space:pre-wrap;}tr:first-child td{font-weight:600;background:#f3f6fa;color:#334155;}`)
	out.WriteString(`.sheet{margin:0 0 28px}.slide-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(320px,1fr));gap:18px}.slide{margin:0}.slide-canvas{aspect-ratio:16/9;overflow:auto;padding:22px 26px;background:#fff;border:1px solid #cfd8e3;border-radius:10px;box-shadow:0 16px 38px rgba(31,41,55,.10);}.slide-canvas small{margin:0 0 16px;text-transform:uppercase;letter-spacing:.08em}.slide-canvas h2{font-size:24px;margin:0 0 16px}.slide-canvas p{font-size:15px;line-height:1.45}`)
	out.WriteString(`</style></head><body><main class="`)
	out.WriteString(html.EscapeString(kindClass))
	out.WriteString(`"><header><h1>`)
	out.WriteString(html.EscapeString(filename))
	out.WriteString(`</h1><small>`)
	out.WriteString(html.EscapeString(subtitle))
	out.WriteString(`</small></header>`)
	out.WriteString(inner)
	out.WriteString(`</main></body></html>`)
	return []byte(out.String())
}

func writeOfficeParagraph(out *strings.Builder, text string) {
	if strings.TrimSpace(text) == "" {
		out.WriteString(`<p class="empty">&nbsp;</p>`)
		return
	}
	out.WriteString(`<p>`)
	out.WriteString(html.EscapeString(text))
	out.WriteString(`</p>`)
}

func writeOfficeTable(out *strings.Builder, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	out.WriteString(`<div class="table-wrap"><table>`)
	for _, row := range rows {
		out.WriteString(`<tr>`)
		if len(row) == 0 {
			out.WriteString(`<td></td>`)
		}
		for _, cell := range row {
			out.WriteString(`<td>`)
			out.WriteString(html.EscapeString(cell))
			out.WriteString(`</td>`)
		}
		out.WriteString(`</tr>`)
	}
	out.WriteString(`</table></div>`)
}

func writeOfficeNotice(out *strings.Builder, text string) {
	out.WriteString(`<div class="notice">`)
	out.WriteString(html.EscapeString(text))
	out.WriteString(`</div>`)
}

func attrValue(start xml.StartElement, localName string) string {
	for _, attr := range start.Attr {
		if attr.Name.Local == localName {
			return attr.Value
		}
	}
	return ""
}

func xlsxColumnIndex(cellRef string) int {
	cellRef = strings.TrimSpace(cellRef)
	if cellRef == "" {
		return -1
	}
	col := 0
	seen := false
	for _, r := range cellRef {
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		if r < 'A' || r > 'Z' {
			break
		}
		seen = true
		col = col*26 + int(r-'A'+1)
	}
	if !seen {
		return -1
	}
	return col - 1
}

func ensureStringSliceLen(values []string, length int) []string {
	for len(values) < length {
		values = append(values, "")
	}
	return values
}

func trimTrailingEmptyCells(values []string) []string {
	end := len(values)
	for end > 0 && strings.TrimSpace(values[end-1]) == "" {
		end--
	}
	return values[:end]
}
