package dxt

import "testing"

func TestGenerateExtensionID(t *testing.T) {
	manifest := Manifest{
		Name: "Claude Tools",
		Author: Author{
			Name: "Anthropic Labs",
		},
	}
	if got := GenerateExtensionID(manifest, "local.dxt"); got != "local.dxt.anthropic-labs.claude-tools" {
		t.Fatalf("unexpected extension id %q", got)
	}
}

func TestParseAndValidateManifestFromText(t *testing.T) {
	manifest, err := ParseAndValidateManifestFromText(`{"name":"Claude Tools","author":{"name":"Anthropic Labs"}}`)
	if err != nil {
		t.Fatalf("ParseAndValidateManifestFromText() error = %v", err)
	}
	if manifest.Author.Name != "Anthropic Labs" {
		t.Fatalf("unexpected manifest %#v", manifest)
	}
}
