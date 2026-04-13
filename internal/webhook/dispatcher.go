package webhook

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/events"
)

type Dispatcher struct {
	hooks     []config.WebhookConfig
	broker    *events.Broker
	delivery  *Delivery
	queue     chan deliveryRequest
	wg        sync.WaitGroup
	startOnce sync.Once
}

type deliveryRequest struct {
	hook  config.WebhookConfig
	event events.Event
}

func NewDispatcher(hooks []config.WebhookConfig, broker *events.Broker, delivery *Delivery) *Dispatcher {
	if delivery == nil {
		delivery = NewDelivery(nil)
	}
	return &Dispatcher{
		hooks:    append([]config.WebhookConfig(nil), hooks...),
		broker:   broker,
		delivery: delivery,
		queue:    make(chan deliveryRequest, 64),
	}
}

func (d *Dispatcher) Start(ctx context.Context) {
	if d == nil || d.broker == nil || len(d.hooks) == 0 {
		return
	}

	d.startOnce.Do(func() {
		_, ch, unsubscribe := d.broker.Subscribe()

		d.wg.Add(2)
		go func() {
			defer d.wg.Done()
			defer unsubscribe()
			defer close(d.queue)
			for {
				select {
				case <-ctx.Done():
					return
				case evt, ok := <-ch:
					if !ok {
						return
					}
					for _, hook := range d.hooks {
						if !matchesAny(hook.Events, evt.Type) {
							continue
						}
						select {
						case d.queue <- deliveryRequest{hook: hook, event: evt}:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}()

		go func() {
			defer d.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case item, ok := <-d.queue:
					if !ok {
						return
					}
					status := d.delivery.Send(ctx, normalizeHook(item.hook), item.event)
					if !status.Success {
						log.Printf("webhook delivery failed: hook=%s event=%s attempts=%d status=%d error=%s", status.Webhook, status.EventType, status.Attempts, status.StatusCode, status.Error)
					}
				}
			}
		}()
	})
}

func (d *Dispatcher) Wait() {
	if d == nil {
		return
	}
	d.wg.Wait()
}

func normalizeHook(hook config.WebhookConfig) config.WebhookConfig {
	if hook.Retry.MaxAttempts <= 0 {
		hook.Retry.MaxAttempts = 3
	}
	if strings.TrimSpace(hook.Format) == "" {
		hook.Format = "json"
	}
	if strings.TrimSpace(hook.Retry.Backoff) == "" {
		hook.Retry.Backoff = "exponential"
	}
	if len(hook.Events) == 0 {
		hook.Events = []string{"*"}
	}
	return hook
}

func matchesAny(patterns []string, eventType string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		switch {
		case pattern == "", pattern == "*":
			return true
		case strings.HasSuffix(pattern, ".*"):
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(eventType, prefix) {
				return true
			}
		case strings.EqualFold(pattern, eventType):
			return true
		}
	}
	return false
}
