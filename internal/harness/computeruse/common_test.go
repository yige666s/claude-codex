package computeruse

import "testing"

func TestFilterAppsForDescription(t *testing.T) {
	apps := []InstalledApp{
		{BundleID: "com.google.Chrome", DisplayName: "Google Chrome", Path: "/Applications/Google Chrome.app"},
		{BundleID: "custom.helper", DisplayName: "Slack Helper", Path: "/Applications/Slack Helper.app"},
		{BundleID: "custom.notes", DisplayName: "Notes+", Path: "/Users/me/Applications/Notes+.app"},
	}
	got := FilterAppsForDescription(apps, "/Users/me")
	if len(got) != 2 || got[0] != "Google Chrome" || got[1] != "Notes+" {
		t.Fatalf("unexpected apps %#v", got)
	}
}

func TestChicagoEnabled(t *testing.T) {
	if ChicagoEnabled("ant", "/repo", "", "max", true) {
		t.Fatal("expected ant with monorepo access to be blocked by default")
	}
	if !ChicagoEnabled("external", "", "", "pro", true) {
		t.Fatal("expected pro tier to allow feature")
	}
}
