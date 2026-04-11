package powershell

import (
	"regexp"
	"strings"
)

type PipelineElementType string
type CommandElementType string
type StatementType string

const (
	PipelineElementCommand           PipelineElementType = "CommandAst"
	PipelineElementCommandExpression PipelineElementType = "CommandExpressionAst"

	CommandElementScriptBlock      CommandElementType = "ScriptBlock"
	CommandElementSubExpression    CommandElementType = "SubExpression"
	CommandElementExpandableString CommandElementType = "ExpandableString"
	CommandElementMemberInvocation CommandElementType = "MemberInvocation"
	CommandElementVariable         CommandElementType = "Variable"
	CommandElementStringConstant   CommandElementType = "StringConstant"
	CommandElementParameter        CommandElementType = "Parameter"
	CommandElementOther            CommandElementType = "Other"

	StatementPipeline StatementType = "PipelineAst"
	StatementUnknown  StatementType = "UnknownStatementAst"
)

type CommandElementChild struct {
	Type CommandElementType
	Text string
}

type ParsedCommandElement struct {
	Name         string
	NameType     string
	ElementType  PipelineElementType
	Args         []string
	Text         string
	ElementTypes []CommandElementType
	Children     [][]CommandElementChild
}

type ParsedStatement struct {
	StatementType    StatementType
	Commands         []ParsedCommandElement
	Redirections     []ParsedRedirection
	Text             string
	NestedCommands   []ParsedCommandElement
	SecurityPatterns SecurityPatterns
}

type ParsedRedirection struct {
	Operator  string
	Target    string
	IsMerging bool
}

type ParsedVariable struct {
	Path       string
	IsSplatted bool
}

type ParseError struct {
	Message string
	ErrorID string
}

type SecurityPatterns struct {
	HasMemberInvocations bool
	HasSubExpressions    bool
	HasExpandableStrings bool
	HasScriptBlocks      bool
}

type ParsedPowerShellCommand struct {
	Valid           bool
	Errors          []ParseError
	Statements      []ParsedStatement
	Variables       []ParsedVariable
	HasStopParsing  bool
	OriginalCommand string
}

var variablePattern = regexp.MustCompile(`(?i)([@$])([a-z_][a-z0-9_:]*)`)

func ParseCommand(command string) ParsedPowerShellCommand {
	command = strings.TrimSpace(command)
	parsed := ParsedPowerShellCommand{
		Valid:           command != "",
		Errors:          nil,
		Statements:      []ParsedStatement{},
		Variables:       extractVariables(command),
		HasStopParsing:  strings.Contains(command, "--%"),
		OriginalCommand: command,
	}
	if command == "" {
		parsed.Errors = []ParseError{{Message: "empty command", ErrorID: "EmptyCommand"}}
		return parsed
	}

	for _, statementText := range splitPowerShellStatements(command) {
		statementType := detectStatementType(statementText)
		statement := ParsedStatement{
			StatementType:    statementType,
			Text:             statementText,
			Commands:         parseStatementCommands(statementText, statementType),
			Redirections:     extractRedirections(statementText),
			NestedCommands:   extractNestedCommands(statementText),
			SecurityPatterns: detectSecurityPatterns(statementText),
		}
		parsed.Statements = append(parsed.Statements, statement)
	}
	return parsed
}

func GetAllCommands(parsed ParsedPowerShellCommand) []ParsedCommandElement {
	var commands []ParsedCommandElement
	for _, statement := range parsed.Statements {
		commands = append(commands, statement.Commands...)
		commands = append(commands, statement.NestedCommands...)
	}
	return commands
}

func GetAllCommandNames(parsed ParsedPowerShellCommand) []string {
	commands := GetAllCommands(parsed)
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, strings.ToLower(command.Name))
	}
	return names
}

func splitPowerShellStatements(command string) []string {
	var parts []string
	var current strings.Builder
	var quote rune
	depth := 0
	for _, ch := range command {
		switch ch {
		case '\'', '"', '`':
			if quote == 0 {
				quote = ch
			} else if quote == ch {
				quote = 0
			}
		case '{', '(', '[':
			if quote == 0 {
				depth++
			}
		case '}', ')', ']':
			if quote == 0 && depth > 0 {
				depth--
			}
		case ';', '\n':
			if quote == 0 && depth == 0 {
				part := strings.TrimSpace(current.String())
				if part != "" {
					parts = append(parts, part)
				}
				current.Reset()
				continue
			}
		}
		current.WriteRune(ch)
	}
	if tail := strings.TrimSpace(current.String()); tail != "" {
		parts = append(parts, tail)
	}
	return parts
}

func parsePipelineCommands(statement string) []ParsedCommandElement {
	var commands []ParsedCommandElement
	for _, segment := range splitOutsideQuotes(statement, '|') {
		command := parseCommandElement(segment)
		if command.Name != "" {
			commands = append(commands, command)
		}
	}
	return commands
}

func parseStatementCommands(statement string, statementType StatementType) []ParsedCommandElement {
	if statementType != StatementPipeline {
		return nil
	}
	return parsePipelineCommands(statement)
}

func parseCommandElement(segment string) ParsedCommandElement {
	text := strings.TrimSpace(segment)
	tokens := splitTokens(text)
	if len(tokens) == 0 {
		return ParsedCommandElement{}
	}
	name := tokens[0]
	args := append([]string(nil), tokens[1:]...)
	elementTypes := make([]CommandElementType, 0, len(tokens))
	children := make([][]CommandElementChild, 0, len(args))
	for index, token := range tokens {
		tokenType := classifyToken(token)
		elementTypes = append(elementTypes, tokenType)
		if index == 0 {
			continue
		}
		children = append(children, classifyChildren(token))
	}
	return ParsedCommandElement{
		Name:         stripQuotes(name),
		NameType:     classifyCommandName(name),
		ElementType:  PipelineElementCommand,
		Args:         normalizeTokens(args),
		Text:         text,
		ElementTypes: elementTypes,
		Children:     children,
	}
}

func extractNestedCommands(statement string) []ParsedCommandElement {
	var nested []ParsedCommandElement
	matches := regexp.MustCompile(`\{([^{}]+)\}`).FindAllStringSubmatch(statement, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		for _, inner := range splitPowerShellStatements(match[1]) {
			nested = append(nested, parsePipelineCommands(inner)...)
		}
	}
	return nested
}

func detectStatementType(statement string) StatementType {
	lower := strings.ToLower(strings.TrimSpace(statement))
	switch {
	case strings.HasPrefix(lower, "if "), strings.HasPrefix(lower, "if("):
		return "IfStatementAst"
	case strings.HasPrefix(lower, "foreach "), strings.HasPrefix(lower, "foreach("):
		return "ForEachStatementAst"
	case strings.HasPrefix(lower, "for "), strings.HasPrefix(lower, "for("):
		return "ForStatementAst"
	case strings.HasPrefix(lower, "while "), strings.HasPrefix(lower, "while("):
		return "WhileStatementAst"
	case strings.HasPrefix(lower, "try "), strings.HasPrefix(lower, "try{"), strings.HasPrefix(lower, "try {"):
		return "TryStatementAst"
	default:
		return StatementPipeline
	}
}

func detectSecurityPatterns(statement string) SecurityPatterns {
	return SecurityPatterns{
		HasMemberInvocations: strings.Contains(statement, "."),
		HasSubExpressions:    strings.Contains(statement, "$("),
		HasExpandableStrings: strings.Contains(statement, "\""),
		HasScriptBlocks:      strings.Contains(statement, "{") && strings.Contains(statement, "}"),
	}
}

func extractRedirections(statement string) []ParsedRedirection {
	var redirections []ParsedRedirection
	for _, token := range splitTokens(statement) {
		switch {
		case strings.HasPrefix(token, "2>&1"):
			redirections = append(redirections, ParsedRedirection{Operator: "2>&1", Target: "1", IsMerging: true})
		case strings.HasPrefix(token, "2>>"):
			redirections = append(redirections, ParsedRedirection{Operator: "2>>", Target: strings.TrimPrefix(token, "2>>")})
		case strings.HasPrefix(token, "2>"):
			redirections = append(redirections, ParsedRedirection{Operator: "2>", Target: strings.TrimPrefix(token, "2>")})
		case strings.HasPrefix(token, ">>"):
			redirections = append(redirections, ParsedRedirection{Operator: ">>", Target: strings.TrimPrefix(token, ">>")})
		case strings.HasPrefix(token, ">"):
			redirections = append(redirections, ParsedRedirection{Operator: ">", Target: strings.TrimPrefix(token, ">")})
		}
	}
	return redirections
}

func extractVariables(command string) []ParsedVariable {
	matches := variablePattern.FindAllStringSubmatch(command, -1)
	var variables []ParsedVariable
	seen := map[string]bool{}
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		key := match[1] + match[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		variables = append(variables, ParsedVariable{
			Path:       match[2],
			IsSplatted: match[1] == "@",
		})
	}
	return variables
}

func splitOutsideQuotes(value string, separator rune) []string {
	var parts []string
	var current strings.Builder
	var quote rune
	for _, ch := range value {
		switch ch {
		case '\'', '"', '`':
			if quote == 0 {
				quote = ch
			} else if quote == ch {
				quote = 0
			}
		}
		if ch == separator && quote == 0 {
			part := strings.TrimSpace(current.String())
			if part != "" {
				parts = append(parts, part)
			}
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}
	if tail := strings.TrimSpace(current.String()); tail != "" {
		parts = append(parts, tail)
	}
	return parts
}

func splitTokens(value string) []string {
	var tokens []string
	var current strings.Builder
	var quote rune
	for _, ch := range value {
		switch ch {
		case '\'', '"', '`':
			if quote == 0 {
				quote = ch
			} else if quote == ch {
				quote = 0
			}
			current.WriteRune(ch)
		case ' ', '\t', '\r', '\n':
			if quote != 0 {
				current.WriteRune(ch)
				continue
			}
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func classifyCommandName(name string) string {
	trimmed := stripQuotes(name)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.ContainsAny(trimmed, `/\`) || strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".ps1"):
		return "application"
	case regexp.MustCompile(`^[A-Za-z]+-[A-Za-z]+$`).MatchString(trimmed):
		return "cmdlet"
	default:
		return "unknown"
	}
}

func classifyToken(token string) CommandElementType {
	switch {
	case strings.HasPrefix(token, "-"):
		return CommandElementParameter
	case strings.HasPrefix(token, "$"), strings.HasPrefix(token, "@"):
		return CommandElementVariable
	case strings.Contains(token, "$("):
		return CommandElementSubExpression
	case strings.Contains(token, "{") && strings.Contains(token, "}"):
		return CommandElementScriptBlock
	case strings.Contains(token, "\""):
		return CommandElementExpandableString
	case strings.Contains(token, "."):
		return CommandElementMemberInvocation
	case isQuoted(token):
		return CommandElementStringConstant
	default:
		return CommandElementStringConstant
	}
}

func classifyChildren(token string) []CommandElementChild {
	if !strings.Contains(token, ":") {
		return nil
	}
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return nil
	}
	return []CommandElementChild{{
		Type: classifyToken(parts[1]),
		Text: parts[1],
	}}
}

func normalizeTokens(tokens []string) []string {
	result := make([]string, 0, len(tokens))
	for _, token := range tokens {
		result = append(result, stripQuotes(token))
	}
	return result
}

func stripQuotes(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func isQuoted(value string) bool {
	return len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\''))
}
