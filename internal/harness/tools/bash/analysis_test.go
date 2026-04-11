package bash

import (
	"reflect"
	"strings"
	"testing"
)

func TestAnalyzeCommandTracksQuoteContextAndDangerousPatterns(t *testing.T) {
	command := "printf 'hidden' \"shown $(whoami)\" # tail comment"

	analysis := AnalyzeCommand(command)

	if strings.Contains(analysis.QuoteContext.WithDoubleQuotes, "hidden") {
		t.Fatalf("single-quoted content should be removed, got %q", analysis.QuoteContext.WithDoubleQuotes)
	}
	if !strings.Contains(analysis.QuoteContext.WithDoubleQuotes, "shown $(whoami)") {
		t.Fatalf("double-quoted content should remain visible, got %q", analysis.QuoteContext.WithDoubleQuotes)
	}
	if !strings.Contains(analysis.QuoteContext.UnquotedKeepQuoteChars, "''") || !strings.Contains(analysis.QuoteContext.UnquotedKeepQuoteChars, "\"\"") {
		t.Fatalf("quote delimiters should remain visible, got %q", analysis.QuoteContext.UnquotedKeepQuoteChars)
	}
	if !analysis.DangerousPatterns.HasCommandSubstitution {
		t.Fatalf("expected command substitution to be detected: %+v", analysis.DangerousPatterns)
	}
	if !analysis.DangerousPatterns.HasComment {
		t.Fatalf("expected trailing shell comment to be detected: %+v", analysis.DangerousPatterns)
	}
}

func TestAnalyzeCommandTracksCompoundOperatorsAndPipelines(t *testing.T) {
	command := `VAR=1 && printf "a|b" | sed 's/a/A/' ; echo done`

	analysis := AnalyzeCommand(command)

	if !analysis.CompoundStructure.HasCompoundOperators {
		t.Fatalf("expected compound operators, got %+v", analysis.CompoundStructure)
	}
	if !analysis.CompoundStructure.HasPipeline {
		t.Fatalf("expected pipeline, got %+v", analysis.CompoundStructure)
	}
	wantOperators := []string{"&&", ";"}
	if !reflect.DeepEqual(analysis.CompoundStructure.Operators, wantOperators) {
		t.Fatalf("operators = %#v, want %#v", analysis.CompoundStructure.Operators, wantOperators)
	}
	wantSegments := []string{`VAR=1`, `printf "a|b" | sed 's/a/A/'`, `echo done`}
	if !reflect.DeepEqual(analysis.CompoundStructure.Segments, wantSegments) {
		t.Fatalf("segments = %#v, want %#v", analysis.CompoundStructure.Segments, wantSegments)
	}
}
