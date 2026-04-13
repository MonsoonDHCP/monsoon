package main

import (
	"bytes"
	"fmt"

	"github.com/monsoondhcp/monsoon/internal/config"
	"gopkg.in/yaml.v3"
)

func mergeConfigPayload(current *config.Config, payload map[string]any) (*config.Config, error) {
	if err := validateConfigPayload(payload); err != nil {
		return nil, err
	}

	base := config.DefaultConfig()
	if current != nil {
		base = current.Clone()
	}

	baseMap, err := configToMap(base)
	if err != nil {
		return nil, err
	}
	mergeMaps(baseMap, payload)

	mergedRaw, err := yaml.Marshal(baseMap)
	if err != nil {
		return nil, fmt.Errorf("marshal merged config: %w", err)
	}

	next := config.DefaultConfig()
	if err := yaml.Unmarshal(mergedRaw, next); err != nil {
		return nil, fmt.Errorf("decode merged config: %w", err)
	}
	return next, nil
}

func validateConfigPayload(payload map[string]any) error {
	if len(payload) == 0 {
		return nil
	}
	raw, err := yaml.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var probe config.Config
	if err := dec.Decode(&probe); err != nil {
		return fmt.Errorf("invalid config payload: %w", err)
	}
	return nil
}

func configToMap(cfg *config.Config) (map[string]any, error) {
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal current config: %w", err)
	}
	out := map[string]any{}
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode current config: %w", err)
	}
	return out, nil
}

func mergeMaps(dst map[string]any, src map[string]any) {
	for key, value := range src {
		srcMap, srcIsMap := value.(map[string]any)
		if !srcIsMap {
			dst[key] = value
			continue
		}
		dstMap, dstIsMap := dst[key].(map[string]any)
		if !dstIsMap {
			dstMap = map[string]any{}
			dst[key] = dstMap
		}
		mergeMaps(dstMap, srcMap)
	}
}
