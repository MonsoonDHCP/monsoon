package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

const treeAudit = "audit"

type Logger struct {
	store *storage.Engine
}

func NewLogger(store *storage.Engine) *Logger {
	return &Logger{store: store}
}

func (l *Logger) Log(_ context.Context, entry Entry) error {
	if l == nil || l.store == nil {
		return nil
	}
	now := time.Now().UTC()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = now
	}
	if strings.TrimSpace(entry.ID) == "" {
		id, err := randomID()
		if err != nil {
			return err
		}
		entry.ID = id
	}
	if strings.TrimSpace(entry.Source) == "" {
		entry.Source = "api"
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	key := []byte(fmt.Sprintf("%020d\x1f%s", entry.Timestamp.UnixNano(), entry.ID))
	return l.store.Put(treeAudit, key, raw)
}

func (l *Logger) Query(_ context.Context, filter QueryFilter) ([]Entry, error) {
	if l == nil || l.store == nil {
		return []Entry{}, nil
	}
	var start, end []byte
	if !filter.From.IsZero() {
		start = []byte(fmt.Sprintf("%020d", filter.From.UTC().UnixNano()))
	}
	if !filter.To.IsZero() {
		end = []byte(fmt.Sprintf("%020d\x1f\xff", filter.To.UTC().UnixNano()))
	}
	out := make([]Entry, 0, 128)
	err := l.store.Iterate(treeAudit, start, end, func(_, value []byte) bool {
		var entry Entry
		if json.Unmarshal(value, &entry) != nil {
			return true
		}
		if !matchesFilter(entry, filter) {
			return true
		}
		out = append(out, entry)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			return false
		}
		return true
	})
	if err != nil && err != storage.ErrNotFound {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	return out, nil
}

func matchesFilter(entry Entry, filter QueryFilter) bool {
	if filter.Actor != "" && !strings.EqualFold(entry.Actor, filter.Actor) {
		return false
	}
	if filter.Action != "" && !strings.EqualFold(entry.Action, filter.Action) {
		return false
	}
	if filter.ObjectType != "" && !strings.EqualFold(entry.ObjectType, filter.ObjectType) {
		return false
	}
	if filter.ObjectID != "" && !strings.EqualFold(entry.ObjectID, filter.ObjectID) {
		return false
	}
	if filter.Source != "" && !strings.EqualFold(entry.Source, filter.Source) {
		return false
	}
	if filter.Query != "" {
		haystack := strings.ToLower(strings.Join([]string{
			entry.Actor,
			entry.Action,
			entry.ObjectType,
			entry.ObjectID,
			entry.Source,
		}, " "))
		if !strings.Contains(haystack, strings.ToLower(strings.TrimSpace(filter.Query))) {
			return false
		}
	}
	return true
}

func randomID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
