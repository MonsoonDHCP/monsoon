package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func atomicWriteFile(path string, raw []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

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

	if err := os.Rename(tmpPath, path); err == nil {
		cleanup = false
		return nil
	}

	backupPath := path + ".bak"
	_ = os.Remove(backupPath)
	movedCurrent := false
	if _, statErr := os.Stat(path); statErr == nil {
		if err := os.Rename(path, backupPath); err != nil {
			return fmt.Errorf("prepare atomic replace: %w", err)
		}
		movedCurrent = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if movedCurrent {
			_ = os.Rename(backupPath, path)
		}
		return err
	}
	if movedCurrent {
		_ = os.Remove(backupPath)
	}
	cleanup = false
	return nil
}
