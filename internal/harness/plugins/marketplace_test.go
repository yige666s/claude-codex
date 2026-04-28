package plugins

import (
	"path/filepath"
	"testing"
)

func TestMarketplaceNameValidationBlocksImpersonation(t *testing.T) {
	for _, name := range []string{"anthropic-marketplace-new", "claude official", "cl\u0430ude-plugins"} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateMarketplaceName(name); err == nil {
				t.Fatalf("expected %q to be rejected", name)
			}
		})
	}
	if err := ValidateMarketplaceName("agent-skills"); err != nil {
		t.Fatalf("official allowlisted marketplace should pass: %v", err)
	}
}

func TestValidateOfficialNameSource(t *testing.T) {
	if err := ValidateOfficialNameSource("agent-skills", MarketplaceSource{
		Source: "github",
		Repo:   "someone/agent-skills",
	}); err == nil {
		t.Fatal("expected reserved marketplace name from third-party repo to fail")
	}
	if err := ValidateOfficialNameSource("agent-skills", MarketplaceSource{
		Source: "github",
		Repo:   "anthropics/agent-skills",
	}); err != nil {
		t.Fatalf("expected official source to pass: %v", err)
	}
}

func TestKnownMarketplacesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_marketplaces.json")
	store := KnownMarketplaceStore{Path: path}
	want := KnownMarketplaces{
		Version: 2,
		Marketplaces: map[string]MarketplaceSource{
			"local": {Source: "local", Path: "/tmp/plugins"},
		},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("save marketplaces: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load marketplaces: %v", err)
	}
	if got.Marketplaces["local"].Path != "/tmp/plugins" {
		t.Fatalf("unexpected marketplaces: %#v", got)
	}
}
