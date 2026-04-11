package voice

import "testing"

func TestCheckDependencies(t *testing.T) {
	status := CheckDependencies()
	if status.Available {
		return
	}
	if len(status.Missing) == 0 {
		t.Fatal("expected missing dependencies when unavailable")
	}
}
