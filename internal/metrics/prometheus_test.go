package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistryExportAndHandler(t *testing.T) {
	reg := NewRegistry()
	reg.IncCounter("monsoon_requests_total", map[string]string{"method": "GET", "path": "/health"}, 2)
	reg.SetGauge("monsoon_up", map[string]string{"instance": "local"}, 1)

	exported := reg.Export()
	for _, fragment := range []string{
		"# TYPE monsoon_requests_total counter",
		`monsoon_requests_total{method="GET",path="/health"} 2.000000`,
		"# TYPE monsoon_up gauge",
		`monsoon_up{instance="local"} 1.000000`,
	} {
		if !strings.Contains(exported, fragment) {
			t.Fatalf("expected export to contain %q, got %q", fragment, exported)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected metrics handler status 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("expected text content type, got %q", ct)
	}
	if body := rr.Body.String(); body != exported {
		t.Fatalf("expected handler body to match export, got %q", body)
	}
}

func TestLabelHelpersRemainStable(t *testing.T) {
	labels := map[string]string{"b": "two", "a": "one"}
	key := labelKey(labels)
	if key != "a=one,b=two" {
		t.Fatalf("unexpected label key %q", key)
	}
	parsed := parseLabelKey(key)
	if parsed["a"] != "one" || parsed["b"] != "two" {
		t.Fatalf("unexpected parsed labels %#v", parsed)
	}
	if got := formatLabels(parsed); got != `a="one",b="two"` {
		t.Fatalf("unexpected formatted labels %q", got)
	}

	rows := renderSeries("metric_name", map[string]float64{
		"":      1,
		"a=one": 2,
	})
	if len(rows) != 2 {
		t.Fatalf("expected two rendered rows, got %d", len(rows))
	}
	if rows[0] != "metric_name 1.000000" {
		t.Fatalf("unexpected first rendered row %q", rows[0])
	}
	if rows[1] != `metric_name{a="one"} 2.000000` {
		t.Fatalf("unexpected second rendered row %q", rows[1])
	}
}
