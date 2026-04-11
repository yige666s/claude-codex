package bash

import "strings"

type heredocSpec struct {
	delimiter string
	stripTabs bool
	quoted    bool
}

const heredocMask = "__HEREDOC_BODY__"

type separatorPredicate func(separator string) bool

func splitTopLevelSegments(command string, shouldSplit separatorPredicate) ([]string, []string) {
	command = maskHeredocBodies(joinEscapedNewlines(command), false)

	var (
		parts     []string
		operators []string
		cur       strings.Builder
	)
	inSingle, inDouble, inComment := false, false, false
	parenDepth, braceDepth, bracketDepth := 0, 0, 0

	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			if s == heredocMask {
				cur.Reset()
				return
			}
			parts = append(parts, s)
		}
		cur.Reset()
	}

	for i := 0; i < len(command); i++ {
		c := command[i]

		if inComment {
			if c == '\n' {
				inComment = false
				if shouldSplit("\n") {
					flush()
					operators = append(operators, "\n")
					continue
				}
				cur.WriteByte(c)
			}
			continue
		}

		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			cur.WriteByte(c)
			continue
		case c == '"' && !inSingle:
			inDouble = !inDouble
			cur.WriteByte(c)
			continue
		case c == '\\':
			cur.WriteByte(c)
			if i+1 < len(command) {
				i++
				cur.WriteByte(command[i])
			}
			continue
		}

		if inSingle || inDouble {
			cur.WriteByte(c)
			continue
		}

		if startsShellComment(command, i) {
			inComment = true
			continue
		}

		if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
			separator, width := detectTopLevelSeparator(command, i)
			if separator != "" && shouldSplit(separator) {
				flush()
				operators = append(operators, separator)
				i += width - 1
				continue
			}
		}

		switch c {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		}

		cur.WriteByte(c)
	}

	flush()
	if len(parts) == 0 {
		return []string{strings.TrimSpace(command)}, operators
	}
	return parts, operators
}

func detectTopLevelSeparator(command string, idx int) (string, int) {
	if idx < 0 || idx >= len(command) {
		return "", 0
	}
	switch command[idx] {
	case '\n':
		return "\n", 1
	case ';':
		return ";", 1
	case '|':
		if idx > 0 && command[idx-1] == '>' {
			return "", 0
		}
		if idx+1 < len(command) {
			switch command[idx+1] {
			case '|':
				return "||", 2
			case '&':
				return "|&", 2
			}
		}
		return "|", 1
	case '&':
		if idx > 0 && command[idx-1] == '>' {
			return "", 0
		}
		if idx+1 < len(command) && command[idx+1] == '&' {
			return "&&", 2
		}
		return "&", 1
	default:
		return "", 0
	}
}

// splitSubcommands splits a compound command string into individual subcommands.
// It ignores separators inside quotes, nested grouping, comments, and heredoc bodies.
func splitSubcommands(command string) []string {
	parts, _ := splitTopLevelSegments(command, func(separator string) bool {
		switch separator {
		case "\n", ";", "|", "|&", "&", "&&", "||":
			return true
		default:
			return false
		}
	})
	return parts
}

// splitCommandTokens splits a command into tokens respecting quotes, comments,
// and escaped whitespace.
func splitCommandTokens(command string) []string {
	command = joinEscapedNewlines(command)

	var tokens []string
	var cur strings.Builder
	inSingle, inDouble := false, false

	flush := func() {
		if cur.Len() == 0 {
			return
		}
		tokens = append(tokens, cur.String())
		cur.Reset()
	}

	for i := 0; i < len(command); i++ {
		c := command[i]

		if !inSingle && !inDouble && startsShellComment(command, i) {
			break
		}

		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			cur.WriteByte(c)
		case c == '"' && !inSingle:
			inDouble = !inDouble
			cur.WriteByte(c)
		case c == '\\':
			cur.WriteByte(c)
			if i+1 < len(command) {
				i++
				cur.WriteByte(command[i])
			}
		case (c == ' ' || c == '\t' || c == '\n') && !inSingle && !inDouble:
			flush()
		default:
			cur.WriteByte(c)
		}
	}

	flush()
	return tokens
}

// stripSafeHeredocSubstitutions strips quoted heredoc bodies from command analysis.
func stripSafeHeredocSubstitutions(command string) string {
	return maskHeredocBodies(command, true)
}

// hasSafeHeredocSubstitution returns true if command contains a quoted heredoc.
func hasSafeHeredocSubstitution(command string) bool {
	for _, spec := range extractHeredocSpecs(command) {
		if spec.quoted {
			return true
		}
	}
	return false
}

func joinEscapedNewlines(command string) string {
	if !strings.Contains(command, "\\\n") {
		return command
	}
	var out strings.Builder
	for i := 0; i < len(command); i++ {
		if command[i] != '\\' {
			out.WriteByte(command[i])
			continue
		}
		j := i
		for j < len(command) && command[j] == '\\' {
			j++
		}
		if j < len(command) && command[j] == '\n' && (j-i)%2 == 1 {
			out.WriteString(strings.Repeat("\\", j-i-1))
			i = j
			continue
		}
		out.WriteString(command[i:j])
		i = j - 1
	}
	return out.String()
}

func maskHeredocBodies(command string, quotedOnly bool) string {
	lines := strings.SplitAfter(command, "\n")
	if len(lines) == 0 {
		return command
	}

	var out strings.Builder
	var pending []heredocSpec

	for _, line := range lines {
		if len(pending) > 0 {
			spec := pending[0]
			if heredocTerminated(line, spec) {
				pending = pending[1:]
				continue
			}
			if quotedOnly && !spec.quoted {
				out.WriteString(line)
			} else {
				out.WriteString(heredocMask)
				if strings.HasSuffix(line, "\n") {
					out.WriteByte('\n')
				}
			}
			continue
		}

		out.WriteString(line)
		pending = append(pending, parseHeredocLineSpecs(line)...)
	}

	return out.String()
}

func extractHeredocSpecs(command string) []heredocSpec {
	lines := strings.SplitAfter(command, "\n")
	var specs []heredocSpec
	for _, line := range lines {
		specs = append(specs, parseHeredocLineSpecs(line)...)
	}
	return specs
}

func parseHeredocLineSpecs(line string) []heredocSpec {
	var specs []heredocSpec
	inSingle, inDouble := false, false

	for i := 0; i < len(line)-1; i++ {
		c := line[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			continue
		case c == '"' && !inSingle:
			inDouble = !inDouble
			continue
		case c == '\\':
			i++
			continue
		}
		if inSingle || inDouble {
			continue
		}
		if startsShellComment(line, i) {
			break
		}
		if c != '<' || line[i+1] != '<' {
			continue
		}

		j := i + 2
		spec := heredocSpec{}
		if j < len(line) && line[j] == '-' {
			spec.stripTabs = true
			j++
		}
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		if j >= len(line) {
			break
		}

		switch line[j] {
		case '\'', '"':
			quote := line[j]
			spec.quoted = true
			j++
			start := j
			for j < len(line) && line[j] != quote {
				j++
			}
			if j >= len(line) {
				return specs
			}
			spec.delimiter = line[start:j]
		default:
			if line[j] == '\\' {
				j++
			}
			start := j
			for j < len(line) && !isShellWhitespace(line[j]) {
				if line[j] == ';' || line[j] == '|' || line[j] == '&' {
					break
				}
				j++
			}
			spec.delimiter = line[start:j]
		}

		if spec.delimiter != "" {
			specs = append(specs, spec)
		}
		i = j
	}

	return specs
}

func heredocTerminated(line string, spec heredocSpec) bool {
	trimmed := strings.TrimRight(line, "\r\n")
	if spec.stripTabs {
		trimmed = strings.TrimLeft(trimmed, "\t")
	}
	return trimmed == spec.delimiter
}

func startsShellComment(s string, idx int) bool {
	if idx < 0 || idx >= len(s) || s[idx] != '#' {
		return false
	}
	if idx == 0 {
		return true
	}
	prev := s[idx-1]
	return prev == ' ' || prev == '\t' || prev == '\n' || prev == ';' || prev == '|' || prev == '&' || prev == '('
}

func isShellWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
