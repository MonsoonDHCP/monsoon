package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	counters map[string]map[string]float64
	gauges   map[string]map[string]float64
}

func NewRegistry() *Registry {
	return &Registry{
		counters: make(map[string]map[string]float64),
		gauges:   make(map[string]map[string]float64),
	}
}

func (r *Registry) IncCounter(name string, labels map[string]string, delta float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	series := ensureSeries(r.counters, name)
	k := labelKey(labels)
	series[k] += delta
}

func (r *Registry) SetGauge(name string, labels map[string]string, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	series := ensureSeries(r.gauges, name)
	k := labelKey(labels)
	series[k] = value
}

func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(r.Export()))
	})
}

func (r *Registry) Export() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var b strings.Builder

	counterNames := mapKeys(r.counters)
	for _, name := range counterNames {
		b.WriteString(fmt.Sprintf("# TYPE %s counter\n", name))
		for _, row := range renderSeries(name, r.counters[name]) {
			b.WriteString(row)
			b.WriteByte('\n')
		}
	}

	gaugeNames := mapKeys(r.gauges)
	for _, name := range gaugeNames {
		b.WriteString(fmt.Sprintf("# TYPE %s gauge\n", name))
		for _, row := range renderSeries(name, r.gauges[name]) {
			b.WriteString(row)
			b.WriteByte('\n')
		}
	}

	return b.String()
}

func ensureSeries(m map[string]map[string]float64, name string) map[string]float64 {
	series, ok := m[name]
	if !ok {
		series = make(map[string]float64)
		m[name] = series
	}
	return series
}

func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := mapKeys(labels)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ",")
}

func parseLabelKey(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			out[kv[0]] = kv[1]
		}
	}
	return out
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := mapKeys(labels)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, labels[k]))
	}
	return strings.Join(parts, ",")
}

func mapKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func renderSeries(name string, values map[string]float64) []string {
	keys := mapKeys(values)
	rows := make([]string, 0, len(keys))
	for _, key := range keys {
		labels := parseLabelKey(key)
		labelPart := formatLabels(labels)
		if labelPart != "" {
			labelPart = "{" + labelPart + "}"
		}
		rows = append(rows, fmt.Sprintf("%s%s %f", name, labelPart, values[key]))
	}
	return rows
}
