package settings

import (
	"context"
	"encoding/json"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

const treeSettings = "settings"
const keyUI = "ui"

type UISettings struct {
	Theme       string `json:"theme"`
	Density     string `json:"density"`
	AutoRefresh bool   `json:"auto_refresh"`
}

func DefaultUISettings() UISettings {
	return UISettings{
		Theme:       "system",
		Density:     "comfortable",
		AutoRefresh: true,
	}
}

type UIStore interface {
	Get(context.Context) (UISettings, error)
	Set(context.Context, UISettings) error
}

type EngineUIStore struct {
	engine *storage.Engine
}

func NewUIStore(engine *storage.Engine) *EngineUIStore {
	return &EngineUIStore{engine: engine}
}

func (s *EngineUIStore) Get(_ context.Context) (UISettings, error) {
	raw, err := s.engine.Get(treeSettings, []byte(keyUI))
	if err != nil {
		if err == storage.ErrNotFound {
			return DefaultUISettings(), nil
		}
		return UISettings{}, err
	}
	var out UISettings
	if err := json.Unmarshal(raw, &out); err != nil {
		return UISettings{}, err
	}
	if out.Theme == "" {
		out.Theme = "system"
	}
	if out.Density == "" {
		out.Density = "comfortable"
	}
	return out, nil
}

func (s *EngineUIStore) Set(_ context.Context, value UISettings) error {
	if value.Theme == "" {
		value.Theme = "system"
	}
	if value.Density == "" {
		value.Density = "comfortable"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.engine.Put(treeSettings, []byte(keyUI), raw)
}
