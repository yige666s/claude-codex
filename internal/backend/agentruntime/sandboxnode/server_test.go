package sandboxnode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClassForOriginFailsClosed(t *testing.T) {
	cases := []struct {
		decision OriginDecision
		want     ExecutionClass
	}{
		{OriginDecision{Origin: OriginRepository}, ClassIsolated},
		{OriginDecision{Origin: OriginRepository, Verified: true, EntryID: "render"}, ClassIsolated},
		{OriginDecision{Origin: OriginExternal, Verified: true, EntryID: "render", EvidenceID: "proof"}, ClassIsolated},
		{OriginDecision{Origin: OriginRepository, Verified: true, EntryID: "render", EvidenceID: "proof"}, ClassNative},
	}
	for _, tc := range cases {
		if got := ClassForOrigin(tc.decision); got != tc.want {
			t.Fatalf("decision %+v: got %s, want %s", tc.decision, got, tc.want)
		}
	}
}

func TestProbeHandlerRequiresAuthAndDoesNotAcceptJobs(t *testing.T) {
	token := strings.Repeat("a", 32)
	handler, err := NewProbeHandler([]byte(token))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, path, auth string
		want               int
	}{
		{"GET", "/v1/health", "", http.StatusUnauthorized},
		{"GET", "/v1/health", "Bearer incorrect", http.StatusUnauthorized},
		{"GET", "/v1/health", "Bearer " + token, http.StatusOK},
		{"GET", "/v1/capabilities", "Bearer " + token, http.StatusOK},
		{"GET", "/v1/prepare", "Bearer " + token, http.StatusNotFound},
		{"POST", "/v1/exec", "Bearer " + token, http.StatusMethodNotAllowed},
		{"POST", "/v1/dev/execute", "Bearer " + token, http.StatusMethodNotAllowed},
		{"POST", "/v1/dev/collect", "Bearer " + token, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.auth != "" {
			req.Header.Set("Authorization", tc.auth)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: got %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
		if tc.path == "/v1/capabilities" {
			var caps Capabilities
			if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
				t.Fatal(err)
			}
			if caps.AcceptingJobs || len(caps.Drivers) != 0 || caps.ProtocolVersion != ProtocolVersion {
				t.Fatalf("capabilities: %+v", caps)
			}
		}
	}
}

func TestProbeHandlerRejectsWeakToken(t *testing.T) {
	if _, err := NewProbeHandler([]byte("short")); err == nil {
		t.Fatal("weak token accepted")
	}
}
