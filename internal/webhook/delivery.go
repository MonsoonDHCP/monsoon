package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/events"
)

type Delivery struct {
	Client *http.Client
}

type DeliveryStatus struct {
	Webhook    string        `json:"webhook"`
	EventType  string        `json:"event_type"`
	Attempts   int           `json:"attempts"`
	Success    bool          `json:"success"`
	StatusCode int           `json:"status_code,omitempty"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"duration"`
}

func NewDelivery(client *http.Client) *Delivery {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Delivery{Client: client}
}

func (d *Delivery) Send(ctx context.Context, hook config.WebhookConfig, evt events.Event) DeliveryStatus {
	started := time.Now()
	status := DeliveryStatus{
		Webhook:   hook.Name,
		EventType: evt.Type,
	}

	maxAttempts := hook.Retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	backoff := strings.ToLower(strings.TrimSpace(hook.Retry.Backoff))
	if backoff == "" {
		backoff = "exponential"
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status.Attempts = attempt
		code, err := d.sendOnce(ctx, hook, evt)
		status.StatusCode = code
		if err == nil && code >= 200 && code < 300 {
			status.Success = true
			status.Duration = time.Since(started)
			return status
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("unexpected status code %d", code)
		}
		if attempt == maxAttempts {
			break
		}
		wait := retryDelay(attempt, backoff)
		select {
		case <-ctx.Done():
			status.Error = ctx.Err().Error()
			status.Duration = time.Since(started)
			return status
		case <-time.After(wait):
		}
	}

	if lastErr != nil {
		status.Error = lastErr.Error()
	}
	status.Duration = time.Since(started)
	return status
}

func (d *Delivery) sendOnce(ctx context.Context, hook config.WebhookConfig, evt events.Event) (int, error) {
	body, contentType, err := renderPayload(hook, evt)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", contentType)
	for key, value := range hook.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	resp, err := d.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func renderPayload(hook config.WebhookConfig, evt events.Event) ([]byte, string, error) {
	switch strings.ToLower(strings.TrimSpace(hook.Format)) {
	case "slack":
		payload := map[string]any{
			"text": fmt.Sprintf("*%s*: `%s`", hook.Name, evt.Type),
			"attachments": []map[string]any{{
				"color": slackColor(evt.Type),
				"fields": []map[string]any{
					{"title": "Event", "value": evt.Type, "short": true},
					{"title": "Timestamp", "value": evt.Timestamp.UTC().Format(time.RFC3339), "short": true},
					{"title": "Data", "value": marshalMapForSlack(evt.Data), "short": false},
				},
			}},
		}
		raw, err := json.Marshal(payload)
		return raw, "application/json", err
	default:
		raw, err := json.Marshal(map[string]any{
			"type":      evt.Type,
			"timestamp": evt.Timestamp.UTC(),
			"data":      evt.Data,
		})
		return raw, "application/json", err
	}
}

func retryDelay(attempt int, mode string) time.Duration {
	if strings.EqualFold(mode, "fixed") {
		return time.Second
	}
	delay := time.Second * time.Duration(1<<(attempt-1))
	if delay > 8*time.Second {
		return 8 * time.Second
	}
	return delay
}

func slackColor(eventType string) string {
	switch {
	case strings.Contains(eventType, "conflict"), strings.Contains(eventType, "rogue"):
		return "danger"
	case strings.Contains(eventType, "expired"), strings.Contains(eventType, "deleted"):
		return "warning"
	default:
		return "good"
	}
}

func marshalMapForSlack(data map[string]any) string {
	if len(data) == 0 {
		return "{}"
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "{}"
	}
	return "```" + string(raw) + "```"
}
