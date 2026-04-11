package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/monsoondhcp/monsoon/internal/api/rest"
	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func configuredBackupDir(cfg *config.Config, dataDirOverride string) string {
	dataDir := cfg.Server.DataDir
	if strings.TrimSpace(dataDirOverride) != "" {
		dataDir = dataDirOverride
	}
	backupDir := cfg.Backup.Auto.Path
	if strings.TrimSpace(backupDir) == "" {
		backupDir = filepath.Join(dataDir, "backups")
	}
	return backupDir
}

func restoreSnapshotIntoEngine(engine *storage.Engine, snapshotPath string) error {
	if engine == nil {
		return fmt.Errorf("storage engine is not available")
	}
	if strings.TrimSpace(snapshotPath) == "" {
		return fmt.Errorf("snapshot path is required")
	}
	return engine.RestoreSnapshot(snapshotPath)
}

func resolveRestorePath(backupDir string, req rest.RestoreRequest) (string, error) {
	backupDir = strings.TrimSpace(backupDir)
	if backupDir == "" {
		return "", fmt.Errorf("backup directory is not configured")
	}
	baseDir, err := filepath.Abs(backupDir)
	if err != nil {
		return "", err
	}

	switch {
	case strings.TrimSpace(req.Name) != "":
		name := strings.TrimSpace(req.Name)
		if filepath.Base(name) != name {
			return "", fmt.Errorf("backup name must not contain path separators")
		}
		return filepath.Join(baseDir, name), nil
	case strings.TrimSpace(req.Path) != "":
		target, err := filepath.Abs(strings.TrimSpace(req.Path))
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(baseDir, target)
		if err != nil {
			return "", err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("restore path must stay within backup directory")
		}
		return target, nil
	default:
		return "", fmt.Errorf("backup name or path is required")
	}
}

func restoreBackupFromRequest(_ context.Context, engine *storage.Engine, cfg *config.Config, dataDirOverride string, req rest.RestoreRequest) (rest.SystemBackup, error) {
	backupDir := configuredBackupDir(cfg, dataDirOverride)
	restorePath, err := resolveRestorePath(backupDir, req)
	if err != nil {
		return rest.SystemBackup{}, err
	}
	if err := restoreSnapshotIntoEngine(engine, restorePath); err != nil {
		return rest.SystemBackup{}, err
	}
	info, err := os.Stat(restorePath)
	if err != nil {
		return rest.SystemBackup{}, err
	}
	abs, _ := filepath.Abs(restorePath)
	if abs != "" {
		restorePath = abs
	}
	return rest.SystemBackup{
		Name:      filepath.Base(restorePath),
		Path:      restorePath,
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime().UTC(),
	}, nil
}
