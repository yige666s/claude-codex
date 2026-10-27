package sandboxnode

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeClientConnectsToAuthenticatedLoopbackNode(t *testing.T) {
	token := strings.Repeat("s", 32)
	handler, err := NewProbeHandler([]byte(token))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewProbeClient(server.URL, token, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	caps, err := client.Capabilities(context.Background())
	if err != nil || caps.AcceptingJobs {
		t.Fatalf("capabilities = %+v, %v", caps, err)
	}
	wrong, err := NewProbeClient(server.URL, strings.Repeat("x", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrong.Health(context.Background()); err == nil {
		t.Fatal("wrong token accepted")
	}
}

func TestProbeClientRejectsPublicHTTPAndURLCredentials(t *testing.T) {
	token := strings.Repeat("a", 32)
	for _, endpoint := range []string{"http://example.com", "http://127.0.0.1:80/path", "https://user:pass@example.com", "https://example.com?secret=x"} {
		if _, err := NewProbeClient(endpoint, token, nil); err == nil {
			t.Fatalf("unsafe endpoint %q accepted", endpoint)
		}
	}
}
