package web

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestFetchToolBrowserlessLiveFallback(t *testing.T) {
	if os.Getenv("AGENT_API_WEBFETCH_BROWSERLESS_LIVE") != "1" {
		t.Skip("set AGENT_API_WEBFETCH_BROWSERLESS_LIVE=1 to run the live Browserless fallback test")
	}
	if strings.TrimSpace(firstEnvValue("AGENT_API_WEBFETCH_BROWSERLESS_API_TOKEN", "BROWSERLESS_API_TOKEN", "BROWSERLESS_TOKEN")) == "" {
		t.Skip("Browserless token is not configured")
	}
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN", "")

	tool := NewFetchTool(nil)
	input, _ := json.Marshal(map[string]any{
		"url":    "https://www.g2.com/products/slack/reviews",
		"prompt": "Return the page title.",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch execute: %v", err)
	}
	if !strings.Contains(result.Output, "fallback: browserless_smart_scrape") {
		_, browserlessErr := tool.fetchBrowserless(context.Background(), fetchInput{
			URL:    "https://www.g2.com/products/slack/reviews",
			Prompt: "Return the page title.",
		}, "https://www.g2.com/products/slack/reviews")
		if browserlessErr != nil {
			t.Fatalf("expected browserless fallback marker; direct output was: %s\nbrowserless direct error: %v", result.Output, browserlessErr)
		}
		t.Fatalf("expected browserless fallback marker, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "source: browserless_smart_scrape") {
		t.Fatalf("expected browserless source marker, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Slack") && !strings.Contains(result.Output, "G2") {
		t.Fatalf("expected fetched G2/Slack content, got: %s", result.Output)
	}
}
