package bash

import "strings"

// QuoteContext mirrors the TS tree-sitter analysis output used by validators.
type QuoteContext struct {
	WithDoubleQuotes       string
	FullyUnquoted          string
	UnquotedKeepQuoteChars string
}

// CompoundStructure captures high-level command composition.
type CompoundStructure struct {
	HasCompoundOperators bool
	HasPipeline          bool
	HasSubshell          bool
	HasCommandGroup      bool
	Operators            []string
	Segments             []string
}

// DangerousPatterns captures security-relevant shell constructs.
type DangerousPatterns struct {
	HasCommandSubstitution bool
	HasProcessSubstitution bool
	HasParameterExpansion  bool
	HasHeredoc             bool
	HasComment             bool
}

// CommandAnalysis is the Go subset of TS treeSitterAnalysis.ts.
type CommandAnalysis struct {
	QuoteContext           QuoteContext
	CompoundStructure      CompoundStructure
	HasActualOperatorNodes bool
	DangerousPatterns      DangerousPatterns
}

func AnalyzeCommand(command string) CommandAnalysis {
	compoundSegments, compoundOperators := splitTopLevelSegments(command, func(separator string) bool {
		switch separator {
		case "\n", ";", "&", "&&", "||":
			return true
		default:
			return false
		}
	})
	pipeSegments, _ := splitTopLevelSegments(command, func(separator string) bool {
		return separator == "|" || separator == "|&"
	})

	return CommandAnalysis{
		QuoteContext:           extractQuoteContext(command),
		CompoundStructure:      buildCompoundStructure(command, compoundSegments, compoundOperators, pipeSegments),
		HasActualOperatorNodes: len(compoundOperators) > 0,
		DangerousPatterns:      extractDangerousPatterns(command),
	}
}

func buildCompoundStructure(command string, segments, operators, pipeSegments []string) CompoundStructure {
	if len(segments) == 0 {
		segments = []string{strings.TrimSpace(command)}
	}

	trimmed := strings.TrimSpace(command)
	unquoted := extractQuoteContext(trimmed).UnquotedKeepQuoteChars
	return CompoundStructure{
		HasCompoundOperators: len(operators) > 0,
		HasPipeline:          len(pipeSegments) > 1,
		HasSubshell:          strings.HasPrefix(trimmed, "(") || strings.Contains(unquoted, " ("),
		HasCommandGroup:      strings.HasPrefix(trimmed, "{") || strings.Contains(unquoted, " {"),
		Operators:            operators,
		Segments:             segments,
	}
}

func extractQuoteContext(command string) QuoteContext {
	command = maskHeredocBodies(joinEscapedNewlines(command), true)

	var (
		withDoubleQuotes       strings.Builder
		fullyUnquoted          strings.Builder
		unquotedKeepQuoteChars strings.Builder
	)

	inSingle, inDouble := false, false
	escapedInDouble := false

	for i := 0; i < len(command); i++ {
		c := command[i]

		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
				unquotedKeepQuoteChars.WriteByte(c)
			}
			continue
		case inDouble:
			if escapedInDouble {
				escapedInDouble = false
				withDoubleQuotes.WriteByte(c)
				continue
			}
			if c == '\\' {
				escapedInDouble = true
				withDoubleQuotes.WriteByte(c)
				continue
			}
			if c == '"' {
				inDouble = false
				unquotedKeepQuoteChars.WriteByte(c)
				continue
			}
			withDoubleQuotes.WriteByte(c)
			continue
		case c == '\'':
			inSingle = true
			unquotedKeepQuoteChars.WriteByte(c)
			continue
		case c == '"':
			inDouble = true
			unquotedKeepQuoteChars.WriteByte(c)
			continue
		default:
			withDoubleQuotes.WriteByte(c)
			fullyUnquoted.WriteByte(c)
			unquotedKeepQuoteChars.WriteByte(c)
		}
	}

	return QuoteContext{
		WithDoubleQuotes:       withDoubleQuotes.String(),
		FullyUnquoted:          fullyUnquoted.String(),
		UnquotedKeepQuoteChars: unquotedKeepQuoteChars.String(),
	}
}

func extractDangerousPatterns(command string) DangerousPatterns {
	quoteContext := extractQuoteContext(command)
	visible := quoteContext.WithDoubleQuotes
	return DangerousPatterns{
		HasCommandSubstitution: strings.Contains(visible, "$(") || hasUnescapedChar(visible, '`'),
		HasProcessSubstitution: strings.Contains(visible, "<(") || strings.Contains(visible, ">(") || strings.Contains(visible, "=("),
		HasParameterExpansion:  strings.Contains(visible, "${"),
		HasHeredoc:             len(extractHeredocSpecs(command)) > 0,
		HasComment:             hasShellComment(command),
	}
}

func hasShellComment(command string) bool {
	inSingle, inDouble := false, false
	for i := 0; i < len(command); i++ {
		switch {
		case command[i] == '\'' && !inDouble:
			inSingle = !inSingle
		case command[i] == '"' && !inSingle:
			inDouble = !inDouble
		case !inSingle && !inDouble && startsShellComment(command, i):
			return true
		case command[i] == '\\':
			i++
		}
	}
	return false
}
