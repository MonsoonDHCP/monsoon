package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/events"
)

func TestDeliveryJSON(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Fatalf("expected auth header")
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	delivery := NewDelivery(server.Client())
	status := delivery.Send(context.Background(), config.WebhookConfig{
		Name:    "json-hook",
		URL:     server.URL,
		Format:  "json",
		Events:  []string{"*"},
		Headers: map[string]string{"Authorization": "Bearer test"},
		Retry:   config.WebhookRetryConfig{MaxAttempts: 1},
	}, events.Event{
		Type:      "lease.released",
		Timestamp: time.Now().UTC(),
		Data:      map[string]any{"ip": "10.0.0.10"},
	})

	if !status.Success {
		t.Fatalf("delivery failed: %+v", status)
	}
	if payload["type"] != "lease.released" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestDeliverySlackRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := calls.Add(1)
		if current < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	delivery := NewDelivery(server.Client())
	status := delivery.Send(context.Background(), config.WebhookConfig{
		Name:   "slack-hook",
		URL:    server.URL,
		Format: "slack",
		Events: []string{"discovery.*"},
		Retry:  config.WebhookRetryConfig{MaxAttempts: 3, Backoff: "fixed"},
	}, events.Event{
		Type:      "discovery.conflict",
		Timestamp: time.Now().UTC(),
		Data:      map[string]any{"ip": "10.0.0.20"},
	})

	if !status.Success {
		t.Fatalf("expected success after retry: %+v", status)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls.Load())
	}
}

func TestDispatcherFiltersEvents(t *testing.T) {
	broker := events.NewBroker(8)
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := NewDispatcher([]config.WebhookConfig{
		{
			Name:   "filtered",
			URL:    server.URL,
			Format: "json",
			Events: []string{"discovery.*"},
			Retry:  config.WebhookRetryConfig{MaxAttempts: 1},
		},
	}, broker, NewDelivery(server.Client()))

	ctx, cancel := context.WithCancel(context.Background())
	dispatcher.Start(ctx)

	broker.Publish(events.Event{Type: "lease.released", Timestamp: time.Now().UTC(), Data: map[string]any{"ip": "10.0.0.30"}})
	broker.Publish(events.Event{Type: "discovery.conflict", Timestamp: time.Now().UTC(), Data: map[string]any{"ip": "10.0.0.31"}})

	time.Sleep(150 * time.Millisecond)
	cancel()
	dispatcher.Wait()

	if received.Load() != 1 {
		t.Fatalf("expected exactly one webhook delivery, got %d", received.Load())
	}
}
