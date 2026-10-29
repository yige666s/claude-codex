package sandboxartifact

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Handler serves artifacts from a private authenticated control-plane route.
// Merely constructing this handler does not expose any production endpoint.
func Handler(store Store, token string) (http.Handler, error) {
	if len(token) < 32 {
		return nil, errors.New("artifact token too short")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/skills/"), "/")
		if !strings.HasPrefix(r.URL.Path, "/v1/skills/") || len(parts) != 2 || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		ref := Ref{ID: parts[0], Digest: parts[1]}
		if err := ref.Validate(); err != nil {
			http.NotFound(w, r)
			return
		}
		data, err := store.Get(ref)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Skill-Digest", ref.Digest)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}), nil
}

type HTTPFetcher struct {
	base            *url.URL
	token           string
	client          *http.Client
	MaxArchiveBytes int64
}

func NewHTTPFetcher(endpoint, token string, maxArchiveBytes int64, client *http.Client) (*HTTPFetcher, error) {
	base, err := url.Parse(endpoint)
	if err != nil || base == nil || base.User != nil || base.Host == "" || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/" && base.Path != "/v1/internal/sandbox") || len(token) < 32 || maxArchiveBytes < 1 {
		return nil, errors.New("invalid artifact source")
	}
	ip := net.ParseIP(base.Hostname())
	if base.Scheme != "https" && !(base.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return nil, errors.New("artifact source requires HTTPS or loopback")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPFetcher{base: base, token: token, client: &copyClient, MaxArchiveBytes: maxArchiveBytes}, nil
}
func (f *HTTPFetcher) Fetch(ctx context.Context, ref Ref) ([]byte, error) {
	if f == nil {
		return nil, errors.New("artifact fetcher unavailable")
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	endpoint := *f.base
	endpoint.Path = strings.TrimSuffix(f.base.Path, "/") + "/v1/skills/" + ref.ID + "/" + ref.Digest
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("skill download returned HTTP %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Skill-Digest") != ref.Digest {
		return nil, errors.New("skill response digest mismatch")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.MaxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > f.MaxArchiveBytes || Digest(data) != ref.Digest {
		return nil, errors.New("skill response size or digest mismatch")
	}
	return data, nil
}
