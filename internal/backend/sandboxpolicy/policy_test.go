package sandboxpolicy

import (
	"strings"
	"testing"
)

func testDocument() Document {
	return Document{Enabled: true, DefaultProfile: "isolated", Profiles: map[string]Profile{
		"isolated": {Driver: "oci", NodePool: "wsl-dev", ImageDigest: "python@sha256:" + strings.Repeat("a", 64), Network: "deny", CPUs: 1, MemoryMiB: 512, PIDs: 128, TimeoutSeconds: 60, MaxOutputBytes: 1024},
	}, Routes: []Route{{Origin: "generated", Profile: "isolated"}}, NodePools: map[string]NodePool{
		"wsl-dev": {Endpoint: "https://example.ngrok.app", CredentialRef: "sandbox-node-dev", Environment: "development", MaxConcurrency: 2},
	}}
}

func TestPolicyFailClosedAndValidate(t *testing.T) {
	if err := DefaultDocument().Validate(); err != nil {
		t.Fatal(err)
	}
	if _, ok := DefaultDocument().ProfileFor("unknown"); ok {
		t.Fatal("disabled policy routed request")
	}
	valid := testDocument()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if got, ok := valid.ProfileFor("external"); !ok || got != "isolated" {
		t.Fatalf("fallback = %q %v", got, ok)
	}
	invalid := testDocument()
	p := invalid.Profiles["isolated"]
	p.Driver = "native"
	invalid.Profiles["isolated"] = p
	if err := invalid.Validate(); err == nil {
		t.Fatal("native profile accepted")
	}
	invalid = testDocument()
	p = invalid.Profiles["isolated"]
	p.Network = "host"
	invalid.Profiles["isolated"] = p
	if err := invalid.Validate(); err == nil {
		t.Fatal("host network accepted")
	}
	invalid = testDocument()
	invalid.NodePools["wsl-dev"] = NodePool{Endpoint: "http://public.example", CredentialRef: "token", Environment: "development", MaxConcurrency: 1}
	if err := invalid.Validate(); err == nil {
		t.Fatal("public HTTP accepted")
	}
	invalid = testDocument()
	invalid.Routes = append(invalid.Routes, Route{Origin: "generated", Profile: "isolated"})
	if err := invalid.Validate(); err == nil {
		t.Fatal("ambiguous route accepted")
	}
	invalid = testDocument()
	invalid.NodePools["wsl-dev"] = NodePool{Endpoint: "https://example.com", CredentialRef: "literal-secret+bad", Environment: "production", MaxConcurrency: 2}
	if err := invalid.Validate(); err == nil {
		t.Fatal("secret-like credential reference accepted")
	}
}

func TestFileOCIProfileRequiresExplicitQuotas(t *testing.T) {
	p := testDocument().Profiles["isolated"]
	if err := ValidateFileOCIProfile(p); err == nil {
		t.Fatal("legacy profile admitted for file execution")
	}
	p.ScratchMiB = 32
	p.MaxArtifactBytes = 1 << 20
	if err := ValidateFileOCIProfile(p); err != nil {
		t.Fatal(err)
	}
	p.ScratchMiB = 300
	if err := ValidateFileOCIProfile(p); err == nil {
		t.Fatal("scratch exceeds cgroup budget")
	}
	p.ScratchMiB = 32
	p.MaxArtifactBytes = 9 << 20
	if err := ValidateFileOCIProfile(p); err == nil {
		t.Fatal("unbounded artifacts accepted")
	}
}
