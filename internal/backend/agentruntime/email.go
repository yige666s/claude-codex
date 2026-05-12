package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := m.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("resend email failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}
