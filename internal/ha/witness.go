package ha

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type witnessRecord struct {
	Node      string    `json:"node"`
	Priority  int       `json:"priority"`
	UpdatedAt time.Time `json:"updated_at"`
}

func readWitnessRecord(path string) (witnessRecord, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return witnessRecord{}, errors.New("witness path is empty")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return witnessRecord{}, err
	}
	var rec witnessRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return witnessRecord{}, err
	}
	rec.Node = strings.TrimSpace(rec.Node)
	return rec, nil
}

func writeWitnessRecord(path string, rec witnessRecord) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("witness path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func witnessOwner(path string, hold time.Duration, now time.Time) (witnessRecord, bool, error) {
	rec, err := readWitnessRecord(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return witnessRecord{}, false, nil
		}
		return witnessRecord{}, false, err
	}
	if rec.Node == "" || rec.UpdatedAt.IsZero() {
		return witnessRecord{}, false, nil
	}
	if hold > 0 && now.Sub(rec.UpdatedAt) > hold {
		return witnessRecord{}, false, nil
	}
	return rec, true, nil
}
