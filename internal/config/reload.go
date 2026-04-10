package config

import (
	"fmt"
	"sync"
)

type Manager struct {
	path      string
	mu        sync.RWMutex
	cfg       *Config
	onReloads []func(*Config)
}

func NewManager(path string) (*Manager, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Manager{path: path, cfg: cfg}, nil
}

func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.Clone()
}

func (m *Manager) RegisterOnReload(fn func(*Config)) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onReloads = append(m.onReloads, fn)
}

func (m *Manager) Reload() error {
	cfg, err := Load(m.path)
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	m.mu.Lock()
	m.cfg = cfg
	callbacks := append([]func(*Config){}, m.onReloads...)
	m.mu.Unlock()

	for _, cb := range callbacks {
		cb(cfg.Clone())
	}

	return nil
}
