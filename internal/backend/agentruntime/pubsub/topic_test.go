package pubsub

import "testing"

func TestTopicPublishesToSubscribers(t *testing.T) {
	topic := NewTopic[int](1, nil)
	updates, unsubscribe := topic.Subscribe("jobs")
	defer unsubscribe()

	topic.Publish("jobs", 42)
	if got := <-updates; got != 42 {
		t.Fatalf("published value = %d", got)
	}
}

func TestTopicDropsSlowSubscriber(t *testing.T) {
	topic := NewTopic[int](1, nil)
	updates, _ := topic.Subscribe("jobs")

	topic.Publish("jobs", 1)
	topic.Publish("jobs", 2)
	if got := <-updates; got != 1 {
		t.Fatalf("first value = %d", got)
	}
	if _, ok := <-updates; ok {
		t.Fatal("slow subscriber should be closed")
	}
}

func TestTopicCloneIsPerSubscriber(t *testing.T) {
	topic := NewTopic[*int](1, func(value *int) *int {
		if value == nil {
			return nil
		}
		clone := *value
		return &clone
	})
	first, firstCancel := topic.Subscribe("jobs")
	defer firstCancel()
	second, secondCancel := topic.Subscribe("jobs")
	defer secondCancel()

	value := 7
	topic.Publish("jobs", &value)
	a := <-first
	b := <-second
	if a == b {
		t.Fatal("expected distinct cloned values")
	}
}
