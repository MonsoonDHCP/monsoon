package audit

import "time"

type Entry struct {
	ID         string         `json:"id"`
	Timestamp  time.Time      `json:"timestamp"`
	Actor      string         `json:"actor"`
	Action     string         `json:"action"`
	ObjectType string         `json:"object_type"`
	ObjectID   string         `json:"object_id"`
	Source     string         `json:"source"`
	Before     map[string]any `json:"before,omitempty"`
	After      map[string]any `json:"after,omitempty"`
	Meta       map[string]any `json:"meta,omitempty"`
}

type QueryFilter struct {
	Actor      string
	Action     string
	ObjectType string
	ObjectID   string
	Source     string
	Query      string
	From       time.Time
	To         time.Time
	Limit      int
}
