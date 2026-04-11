package notifier

import (
	"context"

	publictypes "claude-codex/internal/public/types"
)

type Channel interface {
	Name() string
	Send(context.Context, *publictypes.Notification) error
}

type Service struct {
	channels map[publictypes.NotificationChannel]Channel
}

func NewService() *Service {
	return &Service{channels: map[publictypes.NotificationChannel]Channel{}}
}

func (s *Service) Register(channel publictypes.NotificationChannel, impl Channel) {
	if impl == nil {
		return
	}
	s.channels[channel] = impl
}

func (s *Service) Send(ctx context.Context, cfg publictypes.NotificationConfig, notification *publictypes.Notification) error {
	if !cfg.Enabled || notification == nil {
		return nil
	}
	for _, channel := range cfg.Channels {
		if impl := s.channels[channel]; impl != nil {
			if err := impl.Send(ctx, notification); err != nil {
				return err
			}
		}
	}
	return nil
}
