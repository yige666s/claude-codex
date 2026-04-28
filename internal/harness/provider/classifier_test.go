package provider

import (
	"context"
	"strings"
	"testing"
)

type fakeTextProvider struct {
	request MessageRequest
}

func (p *fakeTextProvider) CreateMessage(_ context.Context, request MessageRequest) (*MessageResponse, error) {
	p.request = request
	return &MessageResponse{
		Content: []ContentBlock{
			{Type: "text", Text: ` {"behavior":"allow"} `},
		},
	}, nil
}

func (p *fakeTextProvider) Name() string { return "fake" }

func (p *fakeTextProvider) SupportedModels() []string { return []string{"fake-model"} }

func TestTextCompletionClient(t *testing.T) {
	provider := &fakeTextProvider{}
	client := TextCompletionClient{
		Provider: provider,
		Model:    "fake-model",
	}

	response, err := client.Complete(context.Background(), "classify this")
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if response != `{"behavior":"allow"}` {
		t.Fatalf("unexpected response: %q", response)
	}
	if provider.request.Model != "fake-model" || provider.request.MaxTokens != 256 {
		t.Fatalf("unexpected request: %+v", provider.request)
	}
	if provider.request.Temperature != 0 || !strings.Contains(provider.request.System, "strict JSON") {
		t.Fatalf("expected deterministic strict JSON request, got %+v", provider.request)
	}
}
