package googleauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestServiceAccountTokenSourceRefreshesAndCachesToken(t *testing.T) {
	var requestCount int
	tokenURI := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/x-www-form-urlencoded") {
			t.Fatalf("content type = %q", got)
		}
		data, err := url.ParseQuery(readBody(t, r))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if data.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Fatalf("grant_type = %q", data.Get("grant_type"))
		}
		assertion := data.Get("assertion")
		parts := strings.Split(assertion, ".")
		if len(parts) != 3 {
			t.Fatalf("unexpected assertion shape: %q", assertion)
		}
		claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			t.Fatalf("decode claims: %v", err)
		}
		var claims map[string]any
		if err := json.Unmarshal(claimsJSON, &claims); err != nil {
			t.Fatalf("parse claims: %v", err)
		}
		if claims["iss"] != "agentapi@example.iam.gserviceaccount.com" || claims["scope"] != CloudPlatformScope || claims["aud"] != tokenURI {
			t.Fatalf("unexpected claims: %#v", claims)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-token","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer server.Close()
	tokenURI = server.URL

	source, err := NewServiceAccountTokenSource(testServiceAccountJSON(t, server.URL), CloudPlatformScope, server.Client())
	if err != nil {
		t.Fatalf("token source: %v", err)
	}
	token, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("access token: %v", err)
	}
	if token != "fresh-token" {
		t.Fatalf("token = %q", token)
	}
	token, err = source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("cached access token: %v", err)
	}
	if token != "fresh-token" || requestCount != 1 {
		t.Fatalf("expected cached token, token=%q requests=%d", token, requestCount)
	}
}

func TestNewServiceAccountTokenSourceFromEnvReadsJSON(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS_JSON", string(testServiceAccountJSON(t, defaultTokenURI)))
	source, ok, err := NewServiceAccountTokenSourceFromEnv(http.DefaultClient)
	if err != nil {
		t.Fatalf("from env: %v", err)
	}
	if !ok || source == nil {
		t.Fatal("expected token source from env")
	}
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	defer r.Body.Close()
	buf := new(strings.Builder)
	if _, err := io.Copy(buf, r.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return buf.String()
}

func testServiceAccountJSON(t *testing.T, tokenURI string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	payload := map[string]string{
		"type":         "service_account",
		"client_email": "agentapi@example.iam.gserviceaccount.com",
		"private_key":  string(pemBlock),
		"token_uri":    tokenURI,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	return data
}
