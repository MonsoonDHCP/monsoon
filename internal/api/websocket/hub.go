package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/monsoondhcp/monsoon/internal/events"
)

type Hub struct {
	broker *events.Broker

	mu      sync.RWMutex
	clients map[*Client]struct{}
}

func NewHub(broker *events.Broker) *Hub {
	return &Hub{
		broker:  broker,
		clients: make(map[*Client]struct{}),
	}
}

func (h *Hub) Start(ctx context.Context) {
	if h == nil || h.broker == nil {
		return
	}
	_, ch, unsubscribe := h.broker.Subscribe()
	go func() {
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				h.closeAll()
				return
			case evt, ok := <-ch:
				if !ok {
					h.closeAll()
					return
				}
				for _, item := range NormalizeEvent(evt) {
					h.Broadcast(item)
				}
			}
		}
	}()
}

func (h *Hub) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := Upgrade(w, r, h)
		if err != nil {
			var handshakeErr handshakeError
			if ok := errorAs(err, &handshakeErr); ok {
				http.Error(w, handshakeErr.Error(), handshakeErr.status)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		h.register(client)
		client.Run()
	})
}

func (h *Hub) Broadcast(message EventMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		client.Send(message)
	}
}

func (h *Hub) register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client] = struct{}{}
}

func (h *Hub) unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, client)
}

func (h *Hub) closeAll() {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()
	for _, client := range clients {
		client.Close()
	}
}

func matchesSubscription(pattern string, eventType string) bool {
	pattern = strings.TrimSpace(pattern)
	switch {
	case pattern == "", pattern == "*":
		return true
	case strings.HasSuffix(pattern, ".*"):
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(eventType, prefix)
	default:
		return strings.EqualFold(pattern, eventType)
	}
}

func marshalEvent(message EventMessage) []byte {
	raw, _ := json.Marshal(message)
	return raw
}

func newSystemMessage(eventType string, data map[string]any) EventMessage {
	return EventMessage{
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		Data:      cloneMap(data),
	}
}

func errorAs(err error, target *handshakeError) bool {
	if err == nil || target == nil {
		return false
	}
	typed, ok := err.(handshakeError)
	if !ok {
		return false
	}
	*target = typed
	return true
}
