package bash

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
)

// Check IDs for analytics (mirrors TS BASH_SECURITY_CHECK_IDS).
const (
	checkIncompleteCommands                 = 1
	checkJqSystemFunction                   = 2
	checkJqFileArguments                    = 3
	checkObfuscatedFlags                    = 4
	checkShellMetacharacters                = 5
	checkDangerousVariables                 = 6
	checkNewlines                           = 7
	checkDangerousPatternsCmdSubstitution   = 8
	checkDangerousPatternsInputRedirection  = 9
	checkDangerousPatternsOutputRedirection = 10
	checkIFSInjection                       = 11
	checkGitCommitSubstitution              = 12
	checkProcEnvironAccess                  = 13
	checkMalformedTokenInjection            = 14
	checkBackslashEscapedWhitespace         = 15
	checkBraceExpansion                     = 16
	checkControlCharacters                  = 17
	checkUnicodeWhitespace                  = 18
	checkMidWordHash                        = 19
	checkZshDangerousCommands               = 20
	checkBackslashEscapedOperators          = 21
	checkCommentQuoteDesync                 = 22
	checkQuotedNewline                      = 23
)

var (
	controlCharRE = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)

	// Shell operators after which backslash-escape causes double-parse bugs.
	shellOperators = map[byte]bool{';': true, '|': true, '&': true, '<': true, '>': true}

	zshDangerousCommands = map[string]bool{
		"zmodload": true, "emulate": true, "sysopen": true, "sysread": true,
		"syswrite": true, "sysseek": true, "zpty": true, "ztcp": true,
		"zsocket": true, "mapfile": true, "zf_rm": true, "zf_mv": true,
		"zf_ln": true, "zf_chmod": true, "zf_chown": true, "zf_mkdir": true,
		"zf_rmdir": true, "zf_chgrp": true,
	}

	cmdSubstitutionPatterns = []struct {
		re  *regexp.Regexp
		msg string
	}{
		{regexp.MustCompile(`<\(`), "process substitution <()"},
		{regexp.MustCompile(`>\(`), "process substitution >()"},
		{regexp.MustCompile(`=\(`), "Zsh process substitution =()"},
		{regexp.MustCompile(`(?:^|[\s;&|])=[a-zA-Z_]`), "Zsh equals expansion (=cmd)"},
		{regexp.MustCompile(`\$\(`), "$() command substitution"},
		{regexp.MustCompile(`\$\{`), "${} parameter substitution"},
		{regexp.MustCompile(`\$\[`), "$[] legacy arithmetic expansion"},
		{regexp.MustCompile(`~\[`), "Zsh-style parameter expansion"},
		{regexp.MustCompile(`\(e:`), "Zsh-style glob qualifiers"},
		{regexp.MustCompile(`\(\+`), "Zsh glob qualifier with command execution"},
		{regexp.MustCompile(`\}\s*always\s*\{`), "Zsh always block"},
		{regexp.MustCompile(`<#`), "PowerShell comment syntax"},
	}

	heredocInSubRE  = regexp.MustCompile(`\$\(.*<<`)
	unicodeWSRE     = regexp.MustCompile(`[\x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]`)
	braceExpRE      = regexp.MustCompile(`\{[^}]+,[^}]*\}|\{[0-9]+\.\.[0-9]+\}`)
	procEnvironRE   = regexp.MustCompile(`/proc/[^/]+/environ`)
	ifsRE           = regexp.MustCompile(`\$(?:IFS|\{[^}]*IFS[^}]*\})`)
	gitCommitSimple = regexp.MustCompile(`^git\s+commit\s+-[a-zA-Z]*m\s+["'][^"'$` + "`" + `(){}|;&\n]+["']\s*$`)
	ansiCQuoteRE    = regexp.MustCompile(`\$'[^']*'`)
	localeQuoteRE   = regexp.MustCompile(`\$"[^"]*"`)
)

// validationContext holds pre-computed command representations for validators.
type validationContext struct {
	original               string
	unquoted               string // double-quote content kept, single-quote content stripped
	fullyUnquoted          string // both quotes stripped + safe redirections removed
	fullyUnquotedPreStrip  string // both quotes stripped, before redirection strip
	unquotedKeepQuoteChars string // content stripped but quote delimiter chars kept
}

func buildValidationContext(command string) validationContext {
	unquoted := stripSingleQuoteContents(command)
	fullyPre := stripBothQuoteContents(command)
	fully := stripSafeRedirections(fullyPre)
	keepChars := stripQuoteContentsKeepDelimiters(command)
	return validationContext{
		original:               command,
		unquoted:               unquoted,
		fullyUnquoted:          fully,
		fullyUnquotedPreStrip:  fullyPre,
		unquotedKeepQuoteChars: keepChars,
	}
}

// BashCommandIsSafe runs all security validators on a command string.
// Returns a PermissionResult indicating allow/ask/passthrough.
func BashCommandIsSafe(command string) permissions.PermissionResult {
	if strings.TrimSpace(command) == "" {
		return permissions.Allow()
	}

	// Control characters — always block
	if controlCharRE.MatchString(command) {
		return permissions.AskMisparsing("command contains control characters")
	}

	// Shell-quote single-quote bug check
	if hasShellQuoteSingleQuoteBug(command) {
		return permissions.AskMisparsing("command may trigger shell-quote single-quote parsing bug")
	}

	// Strip heredoc bodies for the validators
	stripped := stripSafeHeredocSubstitutions(command)

	ctx := buildValidationContext(stripped)

	// Run early validators (can short-circuit to allow)
	if r := validateIncompleteCommands(ctx); r.Behavior != "" {
		if r.Behavior == permissions.BehaviorAllow {
			return r
		}
		return r
	}
	if r := validateSafeCommandSubstitution(ctx); r.Behavior == permissions.BehaviorAllow {
		return r
	}
	if r := validateGitCommit(ctx); r.Behavior != "" {
		return r
	}

	// Deferred (non-misparsing) results — collect and return only if no misparsing check fires
	var deferred *permissions.PermissionResult

	setDeferred := func(r permissions.PermissionResult) {
		if deferred == nil {
			deferred = &r
		}
	}

	// Run main validators in order
	validators := []func(validationContext) permissions.PermissionResult{
		validateJqCommand,
		validateObfuscatedFlags,
		validateShellMetacharacters,
		validateDangerousVariables,
		validateCommentQuoteDesync,
		validateQuotedNewline,
		validateNewlines, // non-misparsing (deferred)
		validateIFSInjection,
		validateProcEnvironAccess,
		validateDangerousPatterns,
		validateRedirections, // non-misparsing (deferred)
		validateBackslashEscapedWhitespace,
		validateBackslashEscapedOperators,
		validateUnicodeWhitespace,
		validateMidWordHash,
		validateBraceExpansion,
		validateZshDangerousCommands,
		validateMalformedTokenInjection,
	}

	for i, v := range validators {
		r := v(ctx)
		if r.Behavior == "" || r.Behavior == permissions.BehaviorAllow {
			continue
		}
		// newlines (idx 6) and redirections (idx 10) are non-misparsing — defer them
		if i == 6 || i == 10 {
			setDeferred(r)
			continue
		}
		// Misparsing check fired — mark and return
		if r.Behavior == permissions.BehaviorAsk {
			r.IsBashSecurityCheckForMisparsing = true
		}
		return r
	}

	if deferred != nil {
		return *deferred
	}
	return permissions.PermissionResult{Behavior: permissions.BehaviorPassthrough}
}

// --- Early validators ---

func validateIncompleteCommands(ctx validationContext) permissions.PermissionResult {
	trimmed := strings.TrimSpace(ctx.original)
	if trimmed == "" {
		return permissions.Allow()
	}
	first := trimmed[0]
	if first == '\t' || first == '-' || shellOperators[first] {
		return permissions.Ask("command starts with incomplete/suspicious token")
	}
	return permissions.PermissionResult{}
}

func validateSafeCommandSubstitution(ctx validationContext) permissions.PermissionResult {
	// Safe heredoc pattern: $(cat <<'EOF'...) — allow it
	if hasSafeHeredocSubstitution(ctx.original) {
		return permissions.Allow()
	}
	return permissions.PermissionResult{}
}

func validateGitCommit(ctx validationContext) permissions.PermissionResult {
	if !strings.HasPrefix(strings.TrimSpace(ctx.original), "git commit") {
		return permissions.PermissionResult{}
	}
	if gitCommitSimple.MatchString(strings.TrimSpace(ctx.original)) {
		return permissions.Allow()
	}
	// Has substitution or operators in message
	if strings.Contains(ctx.original, "$(") || strings.Contains(ctx.original, "${") {
		return permissions.Ask("git commit message contains command substitution")
	}
	return permissions.PermissionResult{}
}

// --- Main validators ---

func validateJqCommand(ctx validationContext) permissions.PermissionResult {
	if !strings.HasPrefix(strings.TrimSpace(ctx.original), "jq") {
		return permissions.PermissionResult{}
	}
	// jq system() function or file arguments
	if strings.Contains(ctx.unquoted, "system(") ||
		regexp.MustCompile(`-f\b|--from-file\b`).MatchString(ctx.unquoted) {
		return permissions.Ask("jq command uses system() or file arguments")
	}
	return permissions.PermissionResult{}
}

func validateObfuscatedFlags(ctx validationContext) permissions.PermissionResult {
	// ANSI-C quoting $'...'
	if ansiCQuoteRE.MatchString(ctx.original) {
		return permissions.Ask("command uses ANSI-C quoting $'...'")
	}
	// Locale quoting $"..."
	if localeQuoteRE.MatchString(ctx.original) {
		return permissions.Ask("command uses locale quoting $\"...\"")
	}
	return permissions.PermissionResult{}
}

func validateShellMetacharacters(ctx validationContext) permissions.PermissionResult {
	// Semicolons or & inside quoted args to find/grep (potential metacharacter injection)
	for _, cmd := range []string{"find ", "grep ", "xargs "} {
		if strings.Contains(ctx.original, cmd) {
			inner := extractQuotedArgsContent(ctx.original)
			if strings.ContainsAny(inner, ";&") {
				return permissions.Ask("shell metacharacters found in quoted arguments to " + strings.TrimSpace(cmd))
			}
		}
	}
	return permissions.PermissionResult{}
}

func validateDangerousVariables(ctx validationContext) permissions.PermissionResult {
	// $VAR adjacent to | < >
	re := regexp.MustCompile(`\$[A-Za-z_][A-Za-z0-9_]*\s*[|<>]|\s*[|<>]\s*\$[A-Za-z_][A-Za-z0-9_]*`)
	if re.MatchString(ctx.fullyUnquoted) {
		return permissions.Ask("dangerous variable adjacent to shell operator")
	}
	return permissions.PermissionResult{}
}

func validateCommentQuoteDesync(ctx validationContext) permissions.PermissionResult {
	// # comment containing quote chars — can desync parser
	re := regexp.MustCompile(`#[^\n]*['"]`)
	if re.MatchString(ctx.original) {
		return permissions.Ask("comment contains quote characters that may desync parser")
	}
	return permissions.PermissionResult{}
}

func validateQuotedNewline(ctx validationContext) permissions.PermissionResult {
	// Newline inside quotes followed by a #-line
	re := regexp.MustCompile(`["'][^"']*\n[^"']*#[^\n]*["']`)
	if re.MatchString(ctx.original) {
		return permissions.AskMisparsing("quoted newline followed by comment may cause misparsing")
	}
	return permissions.PermissionResult{}
}

func validateNewlines(ctx validationContext) permissions.PermissionResult {
	// Unquoted newline followed by non-whitespace (non-misparsing, deferred)
	if strings.Contains(ctx.fullyUnquoted, "\n") {
		after := regexp.MustCompile(`\n\S`)
		if after.MatchString(ctx.fullyUnquoted) {
			return permissions.Ask("command contains unquoted newline before non-whitespace")
		}
	}
	return permissions.PermissionResult{}
}

func validateIFSInjection(ctx validationContext) permissions.PermissionResult {
	if ifsRE.MatchString(ctx.unquoted) {
		return permissions.Ask("command references $IFS which can cause word splitting injection")
	}
	return permissions.PermissionResult{}
}

func validateProcEnvironAccess(ctx validationContext) permissions.PermissionResult {
	if procEnvironRE.MatchString(ctx.unquoted) {
		return permissions.Ask("command accesses /proc/*/environ")
	}
	return permissions.PermissionResult{}
}

func validateDangerousPatterns(ctx validationContext) permissions.PermissionResult {
	// Unescaped backticks
	if hasUnescapedChar(ctx.original, '`') {
		return permissions.Ask("command contains unescaped backtick command substitution")
	}
	// Command substitution patterns
	for _, p := range cmdSubstitutionPatterns {
		if p.re.MatchString(ctx.fullyUnquoted) {
			return permissions.Ask("command contains " + p.msg)
		}
	}
	return permissions.PermissionResult{}
}

func validateRedirections(ctx validationContext) permissions.PermissionResult {
	// Unescaped < or > in fully unquoted content (non-misparsing, deferred)
	if strings.ContainsAny(ctx.fullyUnquoted, "<>") {
		return permissions.Ask("command contains redirections that require permission check")
	}
	return permissions.PermissionResult{}
}

func validateBackslashEscapedWhitespace(ctx validationContext) permissions.PermissionResult {
	if hasBackslashEscapedWhitespace(ctx.original) {
		return permissions.AskMisparsing("command contains \\<space> outside quotes (misparsing risk)")
	}
	return permissions.PermissionResult{}
}

func validateBackslashEscapedOperators(ctx validationContext) permissions.PermissionResult {
	if hasBackslashEscapedOperator(ctx.original) {
		return permissions.AskMisparsing("command contains \\; or \\| outside quotes (misparsing risk)")
	}
	return permissions.PermissionResult{}
}

func validateUnicodeWhitespace(ctx validationContext) permissions.PermissionResult {
	if unicodeWSRE.MatchString(ctx.original) {
		return permissions.Ask("command contains Unicode whitespace characters")
	}
	return permissions.PermissionResult{}
}

func validateMidWordHash(ctx validationContext) permissions.PermissionResult {
	// Non-whitespace immediately before # (unquoted hash that may be a comment)
	re := regexp.MustCompile(`\S#`)
	if re.MatchString(ctx.fullyUnquoted) {
		return permissions.Ask("command contains mid-word hash character")
	}
	return permissions.PermissionResult{}
}

func validateBraceExpansion(ctx validationContext) permissions.PermissionResult {
	if braceExpRE.MatchString(ctx.fullyUnquoted) {
		return permissions.Ask("command uses brace expansion")
	}
	return permissions.PermissionResult{}
}

func validateZshDangerousCommands(ctx validationContext) permissions.PermissionResult {
	words := strings.Fields(ctx.unquoted)
	for _, w := range words {
		if zshDangerousCommands[strings.ToLower(w)] {
			return permissions.Ask("command uses Zsh-specific dangerous command: " + w)
		}
	}
	// fc -e (history execution)
	if regexp.MustCompile(`\bfc\s+-e\b`).MatchString(ctx.unquoted) {
		return permissions.Ask("command uses fc -e (shell history execution)")
	}
	return permissions.PermissionResult{}
}

func validateMalformedTokenInjection(ctx validationContext) permissions.PermissionResult {
	// Unbalanced delimiters combined with command separators
	if hasUnbalancedDelimitersWithSeparators(ctx.fullyUnquoted) {
		return permissions.Ask("command has unbalanced delimiters with command separators")
	}
	return permissions.PermissionResult{}
}

// --- Helper functions ---

// stripSafeRedirections removes safe redirections like 2>&1, >/dev/null, </dev/null.
func stripSafeRedirections(s string) string {
	re := regexp.MustCompile(`\s*2>&1|\s*[12]?>/dev/null|\s*</dev/null`)
	return re.ReplaceAllString(s, "")
}

// stripSingleQuoteContents strips content inside single quotes (keeps double-quote content).
func stripSingleQuoteContents(s string) string {
	var b strings.Builder
	inSingle := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' && !inSingle {
			inSingle = true
			b.WriteByte('\'')
		} else if s[i] == '\'' && inSingle {
			inSingle = false
			b.WriteByte('\'')
		} else if !inSingle {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// stripBothQuoteContents strips content inside both single and double quotes.
func stripBothQuoteContents(s string) string {
	var b strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble && !inSingle:
			inSingle = true
			b.WriteByte(c)
		case c == '\'' && inSingle:
			inSingle = false
			b.WriteByte(c)
		case c == '"' && !inSingle && !inDouble:
			inDouble = true
			b.WriteByte(c)
		case c == '"' && inDouble:
			inDouble = false
			b.WriteByte(c)
		case !inSingle && !inDouble:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// stripQuoteContentsKeepDelimiters strips content but keeps quote delimiter chars.
func stripQuoteContentsKeepDelimiters(s string) string {
	return stripBothQuoteContents(s) // simplified: same as stripBothQuoteContents for delimiter tracking
}

// hasUnescapedChar checks if char c appears unescaped in s.
func hasUnescapedChar(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			// Count preceding backslashes
			count := 0
			for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
				count++
			}
			if count%2 == 0 { // even backslashes = unescaped
				return true
			}
		}
	}
	return false
}

// hasBackslashEscapedWhitespace detects \<space> or \<tab> outside quotes.
func hasBackslashEscapedWhitespace(s string) bool {
	inSingle, inDouble := false, false
	for i := 0; i < len(s)-1; i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '\\' && !inSingle && !inDouble:
			next := s[i+1]
			if next == ' ' || next == '\t' {
				return true
			}
		}
	}
	return false
}

// hasBackslashEscapedOperator detects \; \| \& \< \> outside quotes.
func hasBackslashEscapedOperator(s string) bool {
	inSingle, inDouble := false, false
	for i := 0; i < len(s)-1; i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '\\' && !inSingle && !inDouble:
			next := s[i+1]
			if shellOperators[next] {
				return true
			}
		}
	}
	return false
}

// hasShellQuoteSingleQuoteBug detects cases where shell-quote and bash parse differently.
func hasShellQuoteSingleQuoteBug(s string) bool {
	// Look for \' outside of double quotes — can cause parse divergence
	inDouble := false
	for i := 0; i < len(s)-1; i++ {
		c := s[i]
		if c == '"' {
			inDouble = !inDouble
		}
		if !inDouble && c == '\\' && s[i+1] == '\'' {
			return true
		}
	}
	return false
}

// extractQuotedArgsContent extracts content inside quoted strings.
func extractQuotedArgsContent(s string) string {
	var b strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case inSingle || inDouble:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// hasUnbalancedDelimitersWithSeparators checks for unbalanced delimiters combined with separators.
func hasUnbalancedDelimitersWithSeparators(s string) bool {
	opens := strings.Count(s, "(") + strings.Count(s, "[") + strings.Count(s, "{")
	closes := strings.Count(s, ")") + strings.Count(s, "]") + strings.Count(s, "}")
	if opens != closes && strings.ContainsAny(s, ";|&") {
		return true
	}
	return false
}

// isUnicodeWhitespace returns true if r is a Unicode whitespace that differs between shells.
func isUnicodeWhitespace(r rune) bool {
	if unicode.IsSpace(r) && r != ' ' && r != '\t' && r != '\n' && r != '\r' {
		return true
	}
	// Additional Unicode whitespace ranges
	switch {
	case r == 0x00A0, r == 0x1680:
		return true
	case r >= 0x2000 && r <= 0x200A:
		return true
	case r == 0x2028, r == 0x2029, r == 0x202F, r == 0x205F, r == 0x3000, r == 0xFEFF:
		return true
	}
	return false
}
