package events

import (
	"sync"
	"time"
)

type Event struct {
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

type Broker struct {
	mu      sync.RWMutex
	nextID  int64
	subs    map[int64]chan Event
	bufSize int
}

func NewBroker(bufferSize int) *Broker {
	if bufferSize <= 0 {
		bufferSize = 32
	}
	return &Broker{subs: make(map[int64]chan Event), bufSize: bufferSize}
}

func (b *Broker) Subscribe() (id int64, ch <-chan Event, unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id = b.nextID
	c := make(chan Event, b.bufSize)
	b.subs[id] = c
	return id, c, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if sub, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(sub)
		}
	}
}

func (b *Broker) Publish(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, c := range b.subs {
		select {
		case c <- evt:
		default:
		}
	}
}
