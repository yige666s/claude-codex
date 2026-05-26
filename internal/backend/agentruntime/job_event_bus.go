package agentruntime

import (
	"context"
	"strings"

	"claude-codex/internal/backend/pubsub"
)

type JobEventBus interface {
	JobEventPublisher
	SubscribeJobEvents(jobID string) (<-chan *JobEvent, func())
}

type LocalJobEventBus struct {
	topic *pubsub.Topic[*JobEvent]
}

func NewLocalJobEventBus(bufferDepth int) *LocalJobEventBus {
	return &LocalJobEventBus{
		topic: pubsub.NewTopic(bufferDepth, cloneJobEvent),
	}
}

func (b *LocalJobEventBus) SubscribeJobEvents(jobID string) (<-chan *JobEvent, func()) {
	if b == nil || b.topic == nil || strings.TrimSpace(jobID) == "" {
		ch := make(chan *JobEvent)
		close(ch)
		return ch, func() {}
	}
	return b.topic.Subscribe(jobID)
}

func (b *LocalJobEventBus) PublishJobEvent(_ context.Context, event *JobEvent) error {
	if b == nil || b.topic == nil || event == nil || strings.TrimSpace(event.JobID) == "" {
		return nil
	}
	b.topic.Publish(event.JobID, event)
	return nil
}
