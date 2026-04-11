package diagnostictracking

import "strings"

type Diagnostic struct {
	Message  string
	Severity string
	Range    map[string]any
	Source   string
	Code     string
}

type DiagnosticFile struct {
	URI         string
	Diagnostics []Diagnostic
}

type Service struct {
	baseline map[string][]Diagnostic
}

func NewService() *Service {
	return &Service{baseline: map[string][]Diagnostic{}}
}

func (s *Service) Reset() {
	s.baseline = map[string][]Diagnostic{}
}

func (s *Service) BeforeFileEdited(filePath string, diagnostics []Diagnostic) {
	s.baseline[normalizeFileURI(filePath)] = append([]Diagnostic(nil), diagnostics...)
}

func (s *Service) GetNewDiagnostics(files []DiagnosticFile) []DiagnosticFile {
	var out []DiagnosticFile
	for _, file := range files {
		key := normalizeFileURI(file.URI)
		baseline := s.baseline[key]
		var fresh []Diagnostic
		for _, diagnostic := range file.Diagnostics {
			if !containsDiagnostic(baseline, diagnostic) {
				fresh = append(fresh, diagnostic)
			}
		}
		if len(fresh) > 0 {
			out = append(out, DiagnosticFile{URI: file.URI, Diagnostics: fresh})
		}
	}
	return out
}

func normalizeFileURI(fileURI string) string {
	for _, prefix := range []string{"file://", "_claude_fs_right:", "_claude_fs_left:"} {
		fileURI = strings.TrimPrefix(fileURI, prefix)
	}
	return strings.ToLower(strings.TrimSpace(fileURI))
}

func containsDiagnostic(existing []Diagnostic, candidate Diagnostic) bool {
	for _, diagnostic := range existing {
		if diagnostic.Message == candidate.Message &&
			diagnostic.Severity == candidate.Severity &&
			diagnostic.Source == candidate.Source &&
			diagnostic.Code == candidate.Code {
			return true
		}
	}
	return false
}
