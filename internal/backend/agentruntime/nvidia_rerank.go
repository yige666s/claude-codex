package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"claude-codex/internal/backend/httpclient"
)

const (
	defaultMemoryVectorRerankModel          = "nvidia/llama-nemotron-rerank-1b-v2"
	defaultMemoryVectorRerankCandidateLimit = 50
	defaultMemoryVectorRerankResultLimit    = 5
)

type RerankPassage struct {
	ID   string
	Text string
}

type RerankResult struct {
	Index int
	Score float64
}

type PassageReranker interface {
	Rerank(ctx context.Context, query string, passages []RerankPassage) ([]RerankResult, error)
}

type NVIDIAReranker struct {
	endpoint string
	apiKey   string
	model    string
	truncate string
	client   *http.Client
}

func NewNVIDIAReranker(config MemoryVectorConfig) *NVIDIAReranker {
	config = normalizeMemoryVectorConfig(config)
	endpoint := normalizeNVIDIARerankEndpoint(config.RerankEndpoint, config.RerankModel)
	if endpoint == "" || strings.TrimSpace(config.RerankAPIKey) == "" || strings.TrimSpace(config.RerankModel) == "" {
		return nil
	}
	timeout := config.RerankTimeout
	if timeout <= 0 {
		timeout = config.Timeout
	}
	if timeout <= 0 {
		timeout = defaultMessageSearchTimeout
	}
	truncate := strings.ToUpper(strings.TrimSpace(config.RerankTruncate))
	if truncate == "" {
		truncate = "END"
	}
	return &NVIDIAReranker{
		endpoint: endpoint,
		apiKey:   strings.TrimSpace(config.RerankAPIKey),
		model:    strings.TrimSpace(config.RerankModel),
		truncate: truncate,
		client:   &http.Client{Timeout: timeout},
	}
}

func (r *NVIDIAReranker) Rerank(ctx context.Context, query string, passages []RerankPassage) ([]RerankResult, error) {
	if r == nil || r.endpoint == "" {
		return nil, errMessageSearchNotConfigured("nvidia rerank backend")
	}
	query = strings.TrimSpace(query)
	if query == "" || len(passages) == 0 {
		return []RerankResult{}, nil
	}
	bodyPassages := make([]map[string]string, 0, len(passages))
	for _, passage := range passages {
		text := strings.TrimSpace(passage.Text)
		if text == "" {
			text = strings.TrimSpace(passage.ID)
		}
		if text == "" {
			continue
		}
		bodyPassages = append(bodyPassages, map[string]string{"text": text})
	}
	if len(bodyPassages) == 0 {
		return []RerankResult{}, nil
	}
	body := map[string]any{
		"model":    r.model,
		"query":    map[string]string{"text": query},
		"passages": bodyPassages,
		"truncate": r.truncate,
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+r.apiKey)
	var parsed struct {
		Rankings []struct {
			Index int     `json:"index"`
			Logit float64 `json:"logit"`
		} `json:"rankings"`
	}
	err := httpclient.New(
		httpclient.WithHTTPClient(r.client),
		httpclient.WithComponent("nvidia_rerank"),
	).JSON(ctx, http.MethodPost, r.endpoint, body, &parsed, httpclient.WithHeaders(headers))
	if err != nil {
		var statusErr *httpclient.StatusError
		if errors.As(err, &statusErr) {
			return nil, fmt.Errorf("nvidia rerank failed: %s: %s", statusErr.Status, strings.TrimSpace(statusErr.Body))
		}
		return nil, err
	}
	out := make([]RerankResult, 0, len(parsed.Rankings))
	for _, ranking := range parsed.Rankings {
		if ranking.Index < 0 || ranking.Index >= len(bodyPassages) {
			continue
		}
		out = append(out, RerankResult{Index: ranking.Index, Score: ranking.Logit})
	}
	return out, nil
}

func normalizeNVIDIARerankEndpoint(endpoint, model string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return ""
	}
	if strings.HasSuffix(endpoint, "/ranking") || strings.HasSuffix(endpoint, "/reranking") {
		return endpoint
	}
	model = strings.Trim(strings.TrimSpace(model), "/")
	if strings.Contains(endpoint, "ai.api.nvidia.com") && model != "" {
		if strings.HasSuffix(endpoint, "/v1") {
			return joinEndpointPath(endpoint, "retrieval", model, "reranking")
		}
		return joinEndpointPath(endpoint, "v1", "retrieval", model, "reranking")
	}
	if strings.HasSuffix(endpoint, "/v1") {
		return joinEndpointPath(endpoint, "ranking")
	}
	return joinEndpointPath(endpoint, "v1", "ranking")
}

func rerankConfigured(config MemoryVectorConfig) bool {
	config = normalizeMemoryVectorConfig(config)
	return config.RerankEnabled &&
		strings.TrimSpace(config.RerankEndpoint) != "" &&
		strings.TrimSpace(config.RerankAPIKey) != "" &&
		strings.TrimSpace(config.RerankModel) != ""
}

func rerankResultLimit(config MemoryVectorConfig, requested int) int {
	if requested > 0 {
		return requested
	}
	if config.RerankResultLimit > 0 {
		return config.RerankResultLimit
	}
	return defaultMemoryVectorRerankResultLimit
}

func rerankCandidateLimit(config MemoryVectorConfig, requested int) int {
	limit := config.RerankCandidateLimit
	if limit <= 0 {
		limit = defaultMemoryVectorRerankCandidateLimit
	}
	if requested > limit {
		limit = requested
	}
	if limit > 200 {
		limit = 200
	}
	return limit
}

func normalizeRerankScores(results []RerankResult) map[int]float64 {
	out := make(map[int]float64, len(results))
	if len(results) == 0 {
		return out
	}
	minScore := results[0].Score
	maxScore := results[0].Score
	for _, result := range results {
		if result.Score < minScore {
			minScore = result.Score
		}
		if result.Score > maxScore {
			maxScore = result.Score
		}
	}
	for _, result := range results {
		if maxScore == minScore {
			out[result.Index] = 1
			continue
		}
		out[result.Index] = (result.Score - minScore) / (maxScore - minScore)
	}
	return out
}
