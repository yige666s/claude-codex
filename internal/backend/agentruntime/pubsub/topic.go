package pubsub

import (
	"strings"
	"sync"
)

type Topic[T any] struct {
	mu          sync.Mutex
	bufferDepth int
	clone       func(T) T
	subscribers map[string]map[chan T]struct{}
}

func NewTopic[T any](bufferDepth int, clone func(T) T) *Topic[T] {
	if bufferDepth <= 0 {
		bufferDepth = 128
	}
	return &Topic[T]{
		bufferDepth: bufferDepth,
		clone:       clone,
		subscribers: make(map[string]map[chan T]struct{}),
	}
}

func (t *Topic[T]) Subscribe(topic string) (<-chan T, func()) {
	topic = strings.TrimSpace(topic)
	if t == nil || topic == "" {
		ch := make(chan T)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan T, t.bufferDepth)
	t.mu.Lock()
	if t.subscribers[topic] == nil {
		t.subscribers[topic] = make(map[chan T]struct{})
	}
	t.subscribers[topic][ch] = struct{}{}
	t.mu.Unlock()
	return ch, func() {
		t.unsubscribe(topic, ch)
	}
}

func (t *Topic[T]) Publish(topic string, value T) {
	topic = strings.TrimSpace(topic)
	if t == nil || topic == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	subscribers := t.subscribers[topic]
	for ch := range subscribers {
		item := value
		if t.clone != nil {
			item = t.clone(value)
		}
		select {
		case ch <- item:
		default:
			delete(subscribers, ch)
			close(ch)
		}
	}
	if len(subscribers) == 0 {
		delete(t.subscribers, topic)
	}
}

func (t *Topic[T]) unsubscribe(topic string, ch chan T) {
	t.mu.Lock()
	defer t.mu.Unlock()
	subscribers := t.subscribers[topic]
	if subscribers == nil {
		return
	}
	if _, ok := subscribers[ch]; ok {
		delete(subscribers, ch)
		close(ch)
	}
	if len(subscribers) == 0 {
		delete(t.subscribers, topic)
	}
}
