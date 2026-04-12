package ha

import (
	"encoding/json"
	"errors"
	"fmt"
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, raw, 0o600)
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

func atomicWriteFile(path string, raw []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("replace witness file: %w", err)
		}
		if retryErr := os.Rename(tmpPath, path); retryErr != nil {
			return retryErr
		}
	}
	cleanup = false
	return nil
}
