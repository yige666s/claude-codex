package notifier

import (
	"context"
	"testing"

	publictypes "claude-codex/internal/public/types"
)

type fakeChannel struct{ sent int }

func (f *fakeChannel) Name() string { return "fake" }
func (f *fakeChannel) Send(context.Context, *publictypes.Notification) error {
	f.sent++
	return nil
}

func TestNotifierService(t *testing.T) {
	service := NewService()
	channel := &fakeChannel{}
	service.Register(publictypes.NotificationChannelSlack, channel)
	cfg := publictypes.NotificationConfig{
		Enabled:  true,
		Channels: []publictypes.NotificationChannel{publictypes.NotificationChannelSlack},
	}
	notif := publictypes.NewInfoNotification("title", "message")
	if err := service.Send(context.Background(), cfg, notif); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if channel.sent != 1 {
		t.Fatalf("expected notification to be sent")
	}
}
