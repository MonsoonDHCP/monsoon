package events

import (
	"testing"
	"time"
)

func TestNewBrokerDefaultsBufferSize(t *testing.T) {
	broker := NewBroker(0)
	if broker.bufSize != 32 {
		t.Fatalf("expected default buffer size 32, got %d", broker.bufSize)
	}
}

func TestBrokerPublishSubscribeAndUnsubscribe(t *testing.T) {
	broker := NewBroker(1)
	id, ch, unsubscribe := broker.Subscribe()
	if id != 1 {
		t.Fatalf("expected first subscription id 1, got %d", id)
	}

	before := time.Now().UTC()
	broker.Publish(Event{Type: "lease.created"})

	select {
	case evt := <-ch:
		if evt.Type != "lease.created" {
			t.Fatalf("unexpected event type %q", evt.Type)
		}
		if evt.Timestamp.IsZero() || evt.Timestamp.Before(before) {
			t.Fatalf("expected publish to stamp timestamp, got %v", evt.Timestamp)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	unsubscribe()
	if _, ok := <-ch; ok {
		t.Fatal("expected subscription channel to be closed after unsubscribe")
	}
}

func TestBrokerPublishDropsWhenSubscriberBufferIsFull(t *testing.T) {
	broker := NewBroker(1)
	_, ch, _ := broker.Subscribe()

	broker.Publish(Event{Type: "first"})
	broker.Publish(Event{Type: "second"})

	select {
	case evt := <-ch:
		if evt.Type != "first" {
			t.Fatalf("expected first event to remain buffered, got %q", evt.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for buffered event")
	}

	select {
	case evt := <-ch:
		t.Fatalf("expected second event to be dropped, got %+v", evt)
	default:
	}
}
