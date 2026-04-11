package bash

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitCommandTokensRespectsQuotesEscapesAndComments(t *testing.T) {
	command := `printf foo\ bar "two words" 'three four' # ignored`
	got := splitCommandTokens(command)
	want := []string{`printf`, `foo\ bar`, `"two words"`, `'three four'`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected tokens:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestSplitCommandTokensJoinsEscapedNewlines(t *testing.T) {
	command := "printf foo\\\nbar baz"
	got := splitCommandTokens(command)
	want := []string{"printf", "foobar", "baz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected tokens:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestSplitSubcommandsIgnoresQuotedNestedAndEscapedOperators(t *testing.T) {
	command := `printf "a;b|c" && echo $(printf "x|y") ; echo foo\;bar`
	got := splitSubcommands(command)
	want := []string{
		`printf "a;b|c"`,
		`echo $(printf "x|y")`,
		`echo foo\;bar`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected subcommands:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestSplitSubcommandsIgnoresHeredocBodies(t *testing.T) {
	command := "cat <<'EOF'\nfoo && bar | baz\nEOF\necho done"
	got := splitSubcommands(command)
	want := []string{
		"cat <<'EOF'",
		"echo done",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected subcommands:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestStripSafeHeredocSubstitutionsMasksQuotedBodiesOnly(t *testing.T) {
	command := "cat <<'EOF'\n$(danger)\nEOF\ncat <<EOF\n$ALLOWED\nEOF"
	got := stripSafeHeredocSubstitutions(command)

	if !strings.Contains(got, heredocMask) {
		t.Fatalf("expected quoted heredoc body to be masked, got %q", got)
	}
	if !strings.Contains(got, "$ALLOWED") {
		t.Fatalf("expected unquoted heredoc body to remain visible, got %q", got)
	}
}

func TestHasSafeHeredocSubstitutionRequiresQuotedDelimiter(t *testing.T) {
	if !hasSafeHeredocSubstitution("cat <<'EOF'\nhello\nEOF") {
		t.Fatal("expected quoted heredoc to be considered safe")
	}
	if hasSafeHeredocSubstitution("cat <<EOF\nhello\nEOF") {
		t.Fatal("expected unquoted heredoc to require normal handling")
	}
}

func TestParseCommandExtractsPipeSegmentsAndOutputRedirections(t *testing.T) {
	parsed := ParseCommand(`printf "hello" | sed 's/h/H/' >out.txt 2>>err.log`)

	wantPipes := []string{`printf "hello"`, `sed 's/h/H/' >out.txt 2>>err.log`}
	if got := parsed.PipeSegments(); !reflect.DeepEqual(got, wantPipes) {
		t.Fatalf("PipeSegments() = %#v, want %#v", got, wantPipes)
	}

	wantRedirects := []OutputRedirection{
		{Target: "out.txt", Operator: ">"},
		{Target: "err.log", Operator: ">>"},
	}
	if got := parsed.OutputRedirections(); !reflect.DeepEqual(got, wantRedirects) {
		t.Fatalf("OutputRedirections() = %#v, want %#v", got, wantRedirects)
	}

	if got := parsed.WithoutOutputRedirections(); got != `printf "hello" | sed 's/h/H/'` {
		t.Fatalf("WithoutOutputRedirections() = %q", got)
	}
}
