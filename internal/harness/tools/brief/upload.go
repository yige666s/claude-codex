package brief

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	MaxUploadBytes = 30 * 1024 * 1024
	uploadTimeout  = 30 * time.Second
)

type AuthProvider interface {
	GetAccessToken(ctx context.Context) (string, error)
	BaseAPIURL() string
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Uploader interface {
	UploadAttachment(ctx context.Context, path string, size int64) (string, error)
}

type OAuthUploader struct {
	auth       AuthProvider
	httpClient HTTPDoer
}

type uploadResponse struct {
	FileUUID string `json:"file_uuid"`
}

func NewOAuthUploader(auth AuthProvider, httpClient HTTPDoer) *OAuthUploader {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: uploadTimeout}
	}
	return &OAuthUploader{
		auth:       auth,
		httpClient: httpClient,
	}
}

func UploadEnabledFromEnv() bool {
	return envTruthy(os.Getenv("CLAUDE_CODE_BRIEF_UPLOAD"))
}

func (u *OAuthUploader) UploadAttachment(ctx context.Context, path string, size int64) (string, error) {
	if u == nil || u.auth == nil {
		return "", nil
	}
	if size > MaxUploadBytes {
		return "", nil
	}
	accessToken, err := u.auth.GetAccessToken(ctx)
	if err != nil || strings.TrimSpace(accessToken) == "" {
		return "", nil
	}
	baseURL := strings.TrimRight(strings.TrimSpace(u.auth.BaseAPIURL()), "/")
	if baseURL == "" {
		return "", nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	body, contentType, err := multipartBody(filepath.Base(path), guessMimeType(path), content)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/oauth/file_upload", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Content-Length", fmt.Sprintf("%d", body.Len()))

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", nil
	}
	var decoded uploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", nil
	}
	return strings.TrimSpace(decoded.FileUUID), nil
}

func multipartBody(filename, contentType string, content []byte) (*bytes.Buffer, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(filename)))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(content); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &body, writer.FormDataContentType(), nil
}

func guessMimeType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func escapeQuotes(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

func envTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}
