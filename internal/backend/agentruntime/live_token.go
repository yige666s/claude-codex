package agentruntime

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/googleauth"
)

const defaultVertexLiveGcloudTokenTTL = 50 * time.Minute

type vertexLiveAccessTokenProvider struct {
	httpClient *http.Client

	mu            sync.Mutex
	sourceChecked bool
	source        *googleauth.ServiceAccountTokenSource
	gcloudToken   string
	gcloudExpiry  time.Time
}

func newVertexLiveAccessTokenProvider(httpClient *http.Client) *vertexLiveAccessTokenProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &vertexLiveAccessTokenProvider{httpClient: httpClient}
}

func (p *vertexLiveAccessTokenProvider) AccessToken(ctx context.Context) (string, error) {
	if p == nil {
		return "", fmt.Errorf("live vertex access token provider is nil")
	}
	if token := strings.TrimSpace(firstNonEmpty(
		envString("GOCLAW_VERTEX_ACCESS_TOKEN"),
		envString("VERTEX_ACCESS_TOKEN"),
		envString("GOOGLE_OAUTH_ACCESS_TOKEN"),
		envString("GOOGLE_ACCESS_TOKEN"),
	)); token != "" {
		return token, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sourceChecked {
		source, ok, err := googleauth.NewServiceAccountTokenSourceFromEnv(p.httpClient)
		if err != nil {
			return "", err
		}
		if ok {
			p.source = source
		}
		p.sourceChecked = true
	}
	if p.source != nil {
		return p.source.AccessToken(ctx)
	}
	if p.gcloudToken != "" && time.Until(p.gcloudExpiry) > 5*time.Minute {
		return p.gcloudToken, nil
	}
	token, err := googleauth.GcloudAccessToken(ctx)
	if err != nil {
		return "", err
	}
	p.gcloudToken = token
	p.gcloudExpiry = time.Now().Add(defaultVertexLiveGcloudTokenTTL)
	return token, nil
}
