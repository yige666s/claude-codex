package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"claude-codex/internal/backend/httpclient"
)

type Mailer interface {
	Send(ctx context.Context, message EmailMessage) error
}

type EmailMessage struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

type ResendMailer struct {
	APIKey     string
	From       string
	BaseURL    string
	HTTPClient *http.Client
}

func (m ResendMailer) Send(ctx context.Context, message EmailMessage) error {
	apiKey := strings.TrimSpace(m.APIKey)
	if apiKey == "" {
		return fmt.Errorf("resend API key is required")
	}
	from := strings.TrimSpace(m.From)
	if from == "" {
		return fmt.Errorf("email from address is required")
	}
	to := strings.TrimSpace(message.To)
	if to == "" {
		return fmt.Errorf("email recipient is required")
	}
	subject := strings.TrimSpace(message.Subject)
	if subject == "" {
		return fmt.Errorf("email subject is required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(m.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.resend.com"
	}
	payload := map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
	}
	if strings.TrimSpace(message.HTML) != "" {
		payload["html"] = message.HTML
	}
	if strings.TrimSpace(message.Text) != "" {
		payload["text"] = message.Text
	}
	client := m.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	err := httpclient.New(
		httpclient.WithHTTPClient(client),
		httpclient.WithComponent("resend_mailer"),
		httpclient.WithMaxBodyBytes(4096),
	).JSON(ctx, http.MethodPost, baseURL+"/emails", payload, nil,
		httpclient.WithBearer(apiKey),
	)
	if err != nil {
		var statusErr *httpclient.StatusError
		if errors.As(err, &statusErr) {
			return fmt.Errorf("resend email failed: status %d: %s", statusErr.StatusCode, strings.TrimSpace(statusErr.Body))
		}
		return err
	}
	return nil
}
