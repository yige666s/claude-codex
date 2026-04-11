package deeplink

import "testing"

func TestParseAndBuildDeepLink(t *testing.T) {
	action, err := Parse("claude-cli://open?q=hello+world&repo=owner/repo&cwd=/tmp/project")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if action.Query != "hello world" || action.Repo != "owner/repo" || action.CWD != "/tmp/project" {
		t.Fatalf("unexpected action %#v", action)
	}

	built := Build(action)
	roundTrip, err := Parse(built)
	if err != nil {
		t.Fatalf("Parse(Build()) error = %v", err)
	}
	if roundTrip != action {
		t.Fatalf("round trip mismatch %#v != %#v", roundTrip, action)
	}
}

func TestParseRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{
		"http://example.com",
		"claude-cli://edit",
		"claude-cli://open?repo=owner/repo/extra",
		"claude-cli://open?cwd=relative/path",
	} {
		if _, err := Parse(input); err == nil {
			t.Fatalf("expected %q to fail", input)
		}
	}
}
