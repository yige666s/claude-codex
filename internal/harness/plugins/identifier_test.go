package plugins

import "testing"

func TestParsePluginIdentifierMatchesTypeScriptSemantics(t *testing.T) {
	parsed := ParsePluginIdentifier("demo@market@ignored")
	if parsed.Name != "demo" || parsed.Marketplace != "market" {
		t.Fatalf("unexpected parsed identifier: %#v", parsed)
	}

	parsed = ParsePluginIdentifier("local")
	if parsed.Name != "local" || parsed.Marketplace != "" {
		t.Fatalf("unexpected bare identifier: %#v", parsed)
	}
}

func TestBuildPluginID(t *testing.T) {
	if got := BuildPluginID("demo", "market"); got != "demo@market" {
		t.Fatalf("unexpected plugin ID: %q", got)
	}
	if got := BuildPluginID("demo", ""); got != "demo" {
		t.Fatalf("unexpected bare plugin ID: %q", got)
	}
}
