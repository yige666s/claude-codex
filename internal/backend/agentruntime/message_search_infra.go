package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"claude-codex/internal/backend/googleauth"
	"claude-codex/internal/harness/state"
)

type HTTPMessageFullTextSearcher struct {
	endpoint string
	index    string
	apiKey   string
	username string
	password string
	timeout  time.Duration
	source   string
	client   *http.Client
}

type MessageFullTextIndexer interface {
	IndexMessage(ctx context.Context, message state.Message) error
}

type MessageFullTextDeleter interface {
	DeleteMessage(ctx context.Context, message state.Message) error
}

type HTTPMessageFullTextIndexer struct {
	endpoint string
	index    string
	apiKey   string
	username string
	password string
	timeout  time.Duration
	client   *http.Client
}

func NewHTTPMessageFullTextIndexer(config MessageSearchConfig) *HTTPMessageFullTextIndexer {
	config = normalizeMessageSearchConfig(config)
	return &HTTPMessageFullTextIndexer{
		endpoint: strings.TrimRight(config.Endpoint, "/"),
		index:    strings.TrimSpace(config.IndexWriteAlias),
		apiKey:   config.APIKey,
		username: config.Username,
		password: config.Password,
		timeout:  config.Timeout,
		client:   &http.Client{Timeout: config.Timeout},
	}
}

func (i *HTTPMessageFullTextIndexer) IndexMessage(ctx context.Context, message state.Message) error {
	if i == nil || i.endpoint == "" || i.index == "" {
		return errMessageSearchNotConfigured("full-text indexer")
	}
	if strings.TrimSpace(message.ID) == "" {
		return nil
	}
	if !messageFullTextIndexable(message) {
		return i.deleteMessage(ctx, message.ID)
	}
	document := messageFullTextDocument(message)
	return i.putJSON(ctx, i.documentURL(message.ID), document)
}

func (i *HTTPMessageFullTextIndexer) IndexAttachmentText(ctx context.Context, attachment state.MessageAttachment, text string) error {
	if i == nil || i.endpoint == "" || i.index == "" {
		return errMessageSearchNotConfigured("full-text indexer")
	}
	if !attachmentIndexable(attachment, text) {
		return nil
	}
	chunks := attachmentTextChunks(text, defaultAttachmentChunkSize, defaultAttachmentChunkOverlap)
	for chunkIndex, chunk := range chunks {
		document := messageAttachmentFullTextDocument(attachment, chunk, chunkIndex)
		if err := i.putJSON(ctx, i.documentURL(messageAttachmentDocumentID(attachment, chunkIndex)), document); err != nil {
			return err
		}
	}
	return nil
}

func (i *HTTPMessageFullTextIndexer) DeleteMessage(ctx context.Context, message state.Message) error {
	if i == nil || i.endpoint == "" || i.index == "" {
		return errMessageSearchNotConfigured("full-text indexer")
	}
	if strings.TrimSpace(message.ID) == "" {
		return nil
	}
	if err := i.deleteMessage(ctx, message.ID); err != nil {
		return err
	}
	return i.deleteAttachmentsByMessage(ctx, message)
}

func (i *HTTPMessageFullTextIndexer) DeleteAttachmentText(ctx context.Context, attachment state.MessageAttachment) error {
	if i == nil || i.endpoint == "" || i.index == "" {
		return errMessageSearchNotConfigured("full-text indexer")
	}
	if strings.TrimSpace(attachment.UserID) == "" || strings.TrimSpace(attachment.MessageID) == "" || strings.TrimSpace(attachment.ID) == "" {
		return nil
	}
	body := map[string]any{
		"query": attachmentDeleteQuery(attachment.UserID, attachment.MessageID, attachment.ID),
	}
	return i.postJSONNoDecode(ctx, joinEndpointPath(i.endpoint, i.index, "_delete_by_query"), body)
}

func (i *HTTPMessageFullTextIndexer) putJSON(ctx context.Context, url string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	i.applyAuth(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("message full-text index write failed: %s: %s", resp.Status, readSmallResponse(resp.Body))
	}
	return nil
}

func (i *HTTPMessageFullTextIndexer) deleteMessage(ctx context.Context, messageID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, i.documentURL(messageID), nil)
	if err != nil {
		return err
	}
	i.applyAuth(req)
	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("message full-text index delete failed: %s: %s", resp.Status, readSmallResponse(resp.Body))
	}
	return nil
}

func (i *HTTPMessageFullTextIndexer) deleteAttachmentsByMessage(ctx context.Context, message state.Message) error {
	userID := strings.TrimSpace(message.UserID)
	messageID := strings.TrimSpace(message.ID)
	if userID == "" || messageID == "" {
		return nil
	}
	body := map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []map[string]any{
					{"term": map[string]any{"source_type": messageIndexSourceAttachment}},
					{"term": map[string]any{"user_id": userID}},
					{"term": map[string]any{"message_id": messageID}},
				},
			},
		},
	}
	return i.postJSONNoDecode(ctx, joinEndpointPath(i.endpoint, i.index, "_delete_by_query"), body)
}

func (i *HTTPMessageFullTextIndexer) postJSONNoDecode(ctx context.Context, url string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	i.applyAuth(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("message full-text index delete failed: %s: %s", resp.Status, readSmallResponse(resp.Body))
	}
	return nil
}

func (i *HTTPMessageFullTextIndexer) applyAuth(req *http.Request) {
	if strings.TrimSpace(i.apiKey) != "" {
		req.Header.Set("Authorization", "ApiKey "+strings.TrimSpace(i.apiKey))
	}
	if strings.TrimSpace(i.username) != "" || strings.TrimSpace(i.password) != "" {
		req.SetBasicAuth(i.username, i.password)
	}
}

func (i *HTTPMessageFullTextIndexer) documentURL(messageID string) string {
	return joinEndpointPath(i.endpoint, i.index, "_doc", url.PathEscape(messageID))
}

func NewHTTPMessageFullTextSearcher(config MessageSearchConfig) *HTTPMessageFullTextSearcher {
	config = normalizeMessageSearchConfig(config)
	source := config.Backend
	if source == messageSearchBackendHybrid {
		source = messageSearchBackendElasticsearch
	}
	return &HTTPMessageFullTextSearcher{
		endpoint: strings.TrimRight(config.Endpoint, "/"),
		index:    strings.TrimSpace(config.Index),
		apiKey:   config.APIKey,
		username: config.Username,
		password: config.Password,
		timeout:  config.Timeout,
		source:   source,
		client:   &http.Client{Timeout: config.Timeout},
	}
}

func (s *HTTPMessageFullTextSearcher) SearchMessages(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	if s == nil || s.endpoint == "" {
		return nil, errMessageSearchNotConfigured("full-text backend")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []MessageSearchResult{}, nil
	}
	limit, offset = normalizeSearchPage(limit, offset)
	body := map[string]any{
		"from": offset,
		"size": limit,
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []map[string]any{
					{"term": map[string]any{"user_id": userID}},
					{"term": map[string]any{"status": 1}},
				},
				"must": []map[string]any{
					{"multi_match": map[string]any{
						"query":  query,
						"fields": []string{"content^3", "tool_output", "content_parts.text"},
						"type":   "best_fields",
					}},
				},
				"must_not": []map[string]any{
					{"term": map[string]any{"hidden": true}},
					{"term": map[string]any{"role": "tool"}},
				},
			},
		},
		"sort": []map[string]any{
			{"_score": map[string]any{"order": "desc"}},
			{"created_at": map[string]any{"order": "desc"}},
		},
		"highlight": map[string]any{
			"fields": map[string]any{
				"content":     map[string]any{},
				"tool_output": map[string]any{},
			},
		},
		"_source": []string{"message_id", "session_id", "seq_no", "message_index", "role", "content", "tool_output", "created_at", "session_title"},
	}
	var response elasticSearchResponse
	if err := s.postJSON(ctx, s.searchURL(), body, &response); err != nil {
		return nil, err
	}
	results := make([]MessageSearchResult, 0, len(response.Hits.Hits))
	for _, hit := range response.Hits.Hits {
		result := searchDocumentToResult(hit.Source)
		result.Score = hit.Score
		result.Source = s.source
		if result.Snippet == "" {
			result.Snippet = firstHighlight(hit.Highlight)
		}
		if result.Snippet == "" {
			result.Snippet = messageSearchSnippet(firstNonEmptyString(result.Content, hit.Source.ToolOutput), query, 160)
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *HTTPMessageFullTextSearcher) postJSON(ctx context.Context, url string, payload any, out any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(s.apiKey) != "" {
		req.Header.Set("Authorization", "ApiKey "+strings.TrimSpace(s.apiKey))
	}
	if strings.TrimSpace(s.username) != "" || strings.TrimSpace(s.password) != "" {
		req.SetBasicAuth(s.username, s.password)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("message full-text search failed: %s: %s", resp.Status, readSmallResponse(resp.Body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *HTTPMessageFullTextSearcher) searchURL() string {
	return joinEndpointPath(s.endpoint, s.index, "_search")
}

type OpenAIEmbeddingService struct {
	endpoint   string
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
}

func NewMessageQueryEmbedder(config MessageSearchConfig) QueryEmbedder {
	config = normalizeMessageSearchConfig(config)
	if config.EmbeddingProvider == messageEmbeddingProviderVertex {
		return NewVertexAIEmbeddingService(config)
	}
	return NewOpenAIEmbeddingService(config)
}

func NewOpenAIEmbeddingService(config MessageSearchConfig) *OpenAIEmbeddingService {
	config = normalizeMessageSearchConfig(config)
	endpoint := strings.TrimRight(strings.TrimSpace(config.EmbeddingEndpoint), "/")
	if endpoint != "" && !strings.HasSuffix(endpoint, "/embeddings") {
		if strings.HasSuffix(endpoint, "/v1") {
			endpoint = joinEndpointPath(endpoint, "embeddings")
		} else {
			endpoint = joinEndpointPath(endpoint, "v1", "embeddings")
		}
	}
	return &OpenAIEmbeddingService{
		endpoint:   endpoint,
		apiKey:     strings.TrimSpace(config.EmbeddingAPIKey),
		model:      strings.TrimSpace(config.EmbeddingModel),
		dimensions: config.EmbeddingDimensions,
		client:     &http.Client{Timeout: config.EmbeddingTimeout},
	}
}

func (s *OpenAIEmbeddingService) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if s == nil || s.endpoint == "" {
		return nil, errMessageSearchNotConfigured("embedding backend")
	}
	body := map[string]any{
		"input": query,
	}
	if s.model != "" {
		body["model"] = s.model
	}
	if s.dimensions > 0 {
		body["dimensions"] = s.dimensions
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("message embedding failed: %s: %s", resp.Status, readSmallResponse(resp.Body))
	}
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("message embedding response has no vector")
	}
	vector := make([]float32, len(parsed.Data[0].Embedding))
	for i, value := range parsed.Data[0].Embedding {
		vector[i] = float32(value)
	}
	return vector, nil
}

type QueryEmbedder interface {
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

type VertexAIEmbeddingService struct {
	endpoint     string
	method       string
	accessToken  string
	taskType     string
	dimensions   int
	autoTruncate bool
	client       *http.Client
	tokenSource  func(context.Context) (string, error)
}

func NewVertexAIEmbeddingService(config MessageSearchConfig) *VertexAIEmbeddingService {
	config = normalizeMessageSearchConfig(config)
	timeout := config.EmbeddingTimeout
	if timeout <= 0 {
		timeout = defaultMessageSearchTimeout
	}
	client := &http.Client{Timeout: timeout}
	tokenSource := googleauth.GcloudAccessToken
	if source, ok, err := googleauth.NewServiceAccountTokenSourceFromEnv(client); ok {
		if err != nil {
			tokenSource = func(context.Context) (string, error) {
				return "", err
			}
		} else {
			tokenSource = source.AccessToken
		}
	}
	method := vertexEmbeddingMethod(config.EmbeddingModel)
	return &VertexAIEmbeddingService{
		endpoint:     vertexEmbeddingRequestURL(config, method),
		method:       method,
		accessToken:  strings.TrimSpace(config.EmbeddingAccessToken),
		taskType:     strings.TrimSpace(config.EmbeddingTaskType),
		dimensions:   config.EmbeddingDimensions,
		autoTruncate: config.EmbeddingAutoTruncate,
		client:       client,
		tokenSource:  tokenSource,
	}
}

func (s *VertexAIEmbeddingService) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if s == nil || s.endpoint == "" {
		return nil, errMessageSearchNotConfigured("vertex embedding backend")
	}
	token, err := s.currentToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("vertex embedding access token is required; set GOOGLE_APPLICATION_CREDENTIALS, GOOGLE_APPLICATION_CREDENTIALS_JSON, AGENT_API_MESSAGE_SEARCH_EMBEDDING_TOKEN, VERTEX_ACCESS_TOKEN, or run gcloud auth print-access-token: %w", err)
	}
	body := s.embeddingRequestBody(query)
	vector, statusCode, status, data, err := s.postEmbedding(ctx, token, body)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusUnauthorized && strings.TrimSpace(s.accessToken) == "" {
		token, err = s.refreshToken(ctx)
		if err != nil {
			return nil, err
		}
		vector, statusCode, status, data, err = s.postEmbedding(ctx, token, body)
		if err != nil {
			return nil, err
		}
	}
	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("vertex embedding failed: %s: %s", status, strings.TrimSpace(string(data)))
	}
	if len(vector) == 0 {
		return nil, fmt.Errorf("vertex embedding response has no vector")
	}
	return vector, nil
}

func (s *VertexAIEmbeddingService) embeddingRequestBody(query string) map[string]any {
	if s.method == "embedContent" {
		body := map[string]any{
			"content": map[string]any{
				"role": "user",
				"parts": []map[string]any{
					{"text": query},
				},
			},
			"autoTruncate": s.autoTruncate,
		}
		if s.taskType != "" {
			body["taskType"] = s.taskType
		}
		if s.dimensions > 0 {
			body["outputDimensionality"] = s.dimensions
		}
		return body
	}
	body := map[string]any{
		"instances": []map[string]any{
			{
				"content": query,
			},
		},
	}
	if s.taskType != "" {
		body["instances"].([]map[string]any)[0]["task_type"] = s.taskType
	}
	parameters := map[string]any{
		"autoTruncate": s.autoTruncate,
	}
	if s.dimensions > 0 {
		parameters["outputDimensionality"] = s.dimensions
	}
	body["parameters"] = parameters
	return body
}

func (s *VertexAIEmbeddingService) currentToken(ctx context.Context) (string, error) {
	if strings.TrimSpace(s.accessToken) != "" {
		return strings.TrimSpace(s.accessToken), nil
	}
	return s.refreshToken(ctx)
}

func (s *VertexAIEmbeddingService) refreshToken(ctx context.Context) (string, error) {
	if s == nil || s.tokenSource == nil {
		return "", fmt.Errorf("no token source configured")
	}
	token, err := s.tokenSource(ctx)
	if err != nil {
		return "", err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("token source returned empty token")
	}
	return token, nil
}

func (s *VertexAIEmbeddingService) postEmbedding(ctx context.Context, token string, payload any) ([]float32, int, string, []byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, 0, "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, "", nil, err
	}
	defer resp.Body.Close()
	data, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, resp.Status, data, nil
	}
	values, err := s.parseEmbeddingResponse(data)
	if err != nil {
		return nil, resp.StatusCode, resp.Status, nil, err
	}
	if len(values) == 0 {
		return nil, resp.StatusCode, resp.Status, data, nil
	}
	vector := make([]float32, len(values))
	for i, value := range values {
		vector[i] = float32(value)
	}
	return vector, resp.StatusCode, resp.Status, nil, nil
}

func (s *VertexAIEmbeddingService) parseEmbeddingResponse(data []byte) ([]float64, error) {
	if s.method == "embedContent" {
		var parsed struct {
			Embedding struct {
				Values []float64 `json:"values"`
			} `json:"embedding"`
		}
		if err := json.NewDecoder(bytes.NewReader(data)).Decode(&parsed); err != nil {
			return nil, err
		}
		return parsed.Embedding.Values, nil
	}
	var parsed struct {
		Predictions []struct {
			Embeddings struct {
				Values []float64 `json:"values"`
			} `json:"embeddings"`
		} `json:"predictions"`
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Predictions) == 0 {
		return nil, nil
	}
	return parsed.Predictions[0].Embeddings.Values, nil
}

func vertexEmbeddingRequestURL(config MessageSearchConfig, method string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(config.EmbeddingEndpoint), "/")
	if baseURL == "" {
		baseURL = vertexEmbeddingEndpointBaseURL(config.EmbeddingLocation)
	}
	if strings.HasSuffix(baseURL, ":predict") || strings.HasSuffix(baseURL, ":embedContent") {
		return baseURL
	}
	model := strings.Trim(strings.TrimSpace(config.EmbeddingModel), "/")
	if strings.Contains(model, "/") {
		return fmt.Sprintf("%s/%s:%s", baseURL, model, method)
	}
	projectID := strings.TrimSpace(config.EmbeddingProjectID)
	location := strings.TrimSpace(config.EmbeddingLocation)
	if projectID == "" || location == "" || model == "" {
		return ""
	}
	return fmt.Sprintf("%s/projects/%s/locations/%s/publishers/google/models/%s:%s", baseURL, projectID, location, model, method)
}

func vertexEmbeddingMethod(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(model, "gemini-embedding-2") {
		return "embedContent"
	}
	return "predict"
}

func vertexEmbeddingEndpointBaseURL(location string) string {
	location = strings.ToLower(strings.TrimSpace(location))
	switch location {
	case "global":
		return "https://aiplatform.googleapis.com/v1"
	case "us":
		return "https://aiplatform.us.rep.googleapis.com/v1"
	case "eu":
		return "https://aiplatform.eu.rep.googleapis.com/v1"
	case "":
		return "https://aiplatform.googleapis.com/v1"
	default:
		return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1", location)
	}
}

type QdrantSemanticMessageSearcher struct {
	endpoint       string
	collection     string
	apiKey         string
	scoreThreshold float64
	source         string
	client         *http.Client
	embedder       QueryEmbedder
}

func NewQdrantSemanticMessageSearcher(config MessageSearchConfig) *QdrantSemanticMessageSearcher {
	config = normalizeMessageSearchConfig(config)
	return &QdrantSemanticMessageSearcher{
		endpoint:       strings.TrimRight(strings.TrimSpace(config.QdrantEndpoint), "/"),
		collection:     strings.TrimSpace(config.QdrantCollection),
		apiKey:         strings.TrimSpace(config.QdrantAPIKey),
		scoreThreshold: config.QdrantScoreThreshold,
		source:         "qdrant",
		client:         &http.Client{Timeout: config.Timeout},
		embedder:       NewMessageQueryEmbedder(config),
	}
}

func (s *QdrantSemanticMessageSearcher) SearchSemanticMessages(ctx context.Context, userID, query string, limit int) ([]MessageSearchResult, error) {
	if s == nil || s.endpoint == "" || s.collection == "" {
		return nil, errMessageSearchNotConfigured("qdrant backend")
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	vector, err := s.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": "user_id", "match": map[string]any{"value": userID}},
				{"key": "status", "match": map[string]any{"value": 1}},
			},
			"must_not": []map[string]any{
				{"key": "hidden", "match": map[string]any{"value": true}},
				{"key": "role", "match": map[string]any{"value": "tool"}},
			},
		},
	}
	if s.scoreThreshold > 0 {
		body["score_threshold"] = s.scoreThreshold
	}
	var response qdrantSearchResponse
	if err := s.postJSON(ctx, s.searchURL(), body, &response); err != nil {
		return nil, err
	}
	results := make([]MessageSearchResult, 0, len(response.Result))
	for _, hit := range response.Result {
		result := qdrantPayloadToResult(hit.Payload)
		result.Score = hit.Score
		result.Source = s.source
		if result.Snippet == "" {
			result.Snippet = messageSearchSnippet(result.Content, query, 160)
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *QdrantSemanticMessageSearcher) postJSON(ctx context.Context, url string, payload any, out any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("api-key", s.apiKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("message semantic search failed: %s: %s", resp.Status, readSmallResponse(resp.Body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *QdrantSemanticMessageSearcher) searchURL() string {
	return joinEndpointPath(s.endpoint, "collections", s.collection, "points", "search")
}

type elasticSearchResponse struct {
	Hits struct {
		Hits []struct {
			Score     float64             `json:"_score"`
			Source    messageSearchSource `json:"_source"`
			Highlight map[string][]string `json:"highlight"`
		} `json:"hits"`
	} `json:"hits"`
}

type messageSearchSource struct {
	MessageID    string `json:"message_id"`
	SessionID    string `json:"session_id"`
	SeqNo        int64  `json:"seq_no"`
	MessageIndex int    `json:"message_index"`
	Role         string `json:"role"`
	Content      string `json:"content"`
	ToolOutput   string `json:"tool_output"`
	CreatedAt    string `json:"created_at"`
	SessionTitle string `json:"session_title"`
}

type qdrantSearchResponse struct {
	Result []struct {
		Score   float64        `json:"score"`
		Payload map[string]any `json:"payload"`
	} `json:"result"`
}

func searchDocumentToResult(source messageSearchSource) MessageSearchResult {
	index := source.MessageIndex
	if index == 0 && source.SeqNo > 0 {
		index = int(source.SeqNo - 1)
	}
	return MessageSearchResult{
		MessageID:    source.MessageID,
		SessionID:    source.SessionID,
		MessageIndex: index,
		Role:         source.Role,
		Content:      firstNonEmptyString(source.Content, source.ToolOutput),
		SessionTitle: source.SessionTitle,
		CreatedAt:    parseSearchTime(source.CreatedAt),
	}
}

func qdrantPayloadToResult(payload map[string]any) MessageSearchResult {
	seqNo := int64(searchPayloadNumber(payload, "seq_no"))
	index := int(searchPayloadNumber(payload, "message_index"))
	if index == 0 && seqNo > 0 {
		index = int(seqNo - 1)
	}
	content := searchPayloadString(payload, "content")
	if content == "" {
		content = searchPayloadString(payload, "tool_output")
	}
	return MessageSearchResult{
		MessageID:    searchPayloadString(payload, "message_id"),
		SessionID:    searchPayloadString(payload, "session_id"),
		MessageIndex: index,
		Role:         searchPayloadString(payload, "role"),
		Content:      content,
		SessionTitle: searchPayloadString(payload, "session_title"),
		CreatedAt:    parseSearchTime(searchPayloadString(payload, "created_at")),
	}
}

func messageFullTextIndexable(message state.Message) bool {
	if strings.TrimSpace(message.ID) == "" || strings.TrimSpace(message.UserID) == "" || strings.TrimSpace(message.SessionID) == "" {
		return false
	}
	if message.Hidden || message.Role == state.MessageRoleTool {
		return false
	}
	status := normalizedMessageStatus(message.Status)
	if status != state.MessageStatusNormal {
		return false
	}
	return strings.TrimSpace(messageFullTextSearchableText(message)) != ""
}

func messageFullTextDocument(message state.Message) map[string]any {
	contentParts := messageFullTextContentParts(message)
	content := strings.TrimSpace(message.Content)
	if content == "" {
		content = messageFullTextSearchableText(message)
	}
	return map[string]any{
		"message_id":    message.ID,
		"source_type":   messageIndexSourceMessage,
		"session_id":    message.SessionID,
		"user_id":       message.UserID,
		"seq_no":        message.SeqNo,
		"message_index": maxInt(int(message.SeqNo-1), 0),
		"role":          message.Role,
		"status":        normalizedMessageStatus(message.Status),
		"hidden":        message.Hidden,
		"created_at":    message.CreatedAt.UTC().Format(time.RFC3339Nano),
		"session_title": "",
		"content":       content,
		"tool_output":   strings.TrimSpace(message.ToolOutput),
		"content_parts": contentParts,
	}
}

func messageAttachmentFullTextDocument(attachment state.MessageAttachment, text string, chunkIndex int) map[string]any {
	createdAt := attachment.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	content := attachmentIndexText(attachment, text)
	return map[string]any{
		"message_id":    attachment.MessageID,
		"attachment_id": attachment.ID,
		"source_type":   messageIndexSourceAttachment,
		"session_id":    attachment.SessionID,
		"user_id":       attachment.UserID,
		"seq_no":        0,
		"message_index": 0,
		"chunk_index":   chunkIndex,
		"role":          messageIndexSourceAttachment,
		"status":        state.MessageStatusNormal,
		"hidden":        false,
		"created_at":    createdAt.UTC().Format(time.RFC3339Nano),
		"session_title": "",
		"content":       content,
		"tool_output":   "",
		"file_name":     strings.TrimSpace(attachment.FileName),
		"file_type":     strings.TrimSpace(attachment.FileType),
		"mime_type":     strings.TrimSpace(attachment.MimeType),
		"content_parts": []map[string]any{
			{
				"type":      messageIndexSourceAttachment,
				"text":      content,
				"file_name": strings.TrimSpace(attachment.FileName),
			},
		},
	}
}

func attachmentDeleteQuery(userID, messageID, attachmentID string) map[string]any {
	return map[string]any{
		"bool": map[string]any{
			"filter": []map[string]any{
				{"term": map[string]any{"source_type": messageIndexSourceAttachment}},
				{"term": map[string]any{"user_id": strings.TrimSpace(userID)}},
				{"term": map[string]any{"message_id": strings.TrimSpace(messageID)}},
				{"term": map[string]any{"attachment_id": strings.TrimSpace(attachmentID)}},
			},
		},
	}
}

func messageFullTextSearchableText(message state.Message) string {
	return messageVectorIndexText(message)
}

func messageFullTextContentParts(message state.Message) []map[string]any {
	blocks := message.ContentParts
	if len(blocks) == 0 {
		blocks = message.ContentBlocks
	}
	parts := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		text := strings.TrimSpace(firstNonEmptyString(block.Text, block.Content))
		if text == "" {
			continue
		}
		parts = append(parts, map[string]any{
			"type": strings.TrimSpace(block.Type),
			"text": text,
		})
	}
	return parts
}

func searchPayloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func searchPayloadNumber(payload map[string]any, key string) float64 {
	value, ok := payload[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(typed, 64)
		return parsed
	default:
		return 0
	}
}

func firstHighlight(highlight map[string][]string) string {
	for _, key := range []string{"content", "tool_output"} {
		values := highlight[key]
		if len(values) == 0 {
			continue
		}
		return cleanSearchHighlight(values[0])
	}
	return ""
}

func cleanSearchHighlight(value string) string {
	replacer := strings.NewReplacer("<em>", "", "</em>", "", "\n", " ")
	return strings.TrimSpace(replacer.Replace(value))
}

func parseSearchTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func joinEndpointPath(endpoint string, parts ...string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(endpoint, "/") + "/" + strings.Join(parts, "/")
	}
	cleanParts := []string{strings.Trim(parsed.Path, "/")}
	for _, part := range parts {
		if trimmed := strings.Trim(part, "/"); trimmed != "" {
			cleanParts = append(cleanParts, trimmed)
		}
	}
	joined := path.Join(cleanParts...)
	if joined != "" && !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	parsed.Path = joined
	return parsed.String()
}

func readSmallResponse(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, 4096))
	return strings.TrimSpace(string(data))
}
