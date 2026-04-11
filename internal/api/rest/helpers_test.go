package rest

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type errorCodeRecorder struct {
	*httptest.ResponseRecorder
	code string
}

func (r *errorCodeRecorder) SetErrorCode(code string) { r.code = code }

func TestWriteJSONAndWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, map[string]any{"ok": true}, map[string]any{"page": 1})
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type: %s", got)
	}
	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal json envelope: %v", err)
	}
	if rec.Code != http.StatusCreated || env.Error != nil {
		t.Fatalf("unexpected json response: code=%d env=%+v", rec.Code, env)
	}

	errRec := &errorCodeRecorder{ResponseRecorder: httptest.NewRecorder()}
	WriteError(errRec, http.StatusUnauthorized, "unauthorized", "denied")
	if errRec.code != "unauthorized" {
		t.Fatalf("expected error code setter to be called, got %q", errRec.code)
	}
	if errRec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected error status: %d", errRec.Code)
	}
	if err := json.Unmarshal(errRec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal error envelope: %v", err)
	}
	if env.Error == nil || env.Error.Code != "unauthorized" || env.Error.Message != "denied" {
		t.Fatalf("unexpected error envelope: %+v", env)
	}
}

func TestDashboardHandlerServesAssetsAndFallbackIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('ok');"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	handler, err := NewSPADashboardHandler(dir, "/ui", "/metrics")
	if err != nil {
		t.Fatalf("NewSPADashboardHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/app.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "console.log") {
		t.Fatalf("expected asset response, got code=%d body=%q", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/ui/deep/link", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "index") {
		t.Fatalf("expected index fallback, got code=%d body=%q", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodHead, "/ui/deep/link", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("expected head fallback without body, got code=%d len=%d", rec.Code, rec.Body.Len())
	}

	req = httptest.NewRequest(http.MethodPost, "/ui", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected method not allowed, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected api passthrough 404, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected metrics passthrough 404, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/other", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected base-path 404, got %d", rec.Code)
	}
}

func TestDashboardHelpers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('ok');"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	_, openFile, err := resolveDashboardSource(dir)
	if err != nil {
		t.Fatalf("resolveDashboardSource(dir): %v", err)
	}
	ok, err := assetExists(openFile, "/assets/app.js")
	if err != nil || !ok {
		t.Fatalf("assetExists(file) = %v err=%v", ok, err)
	}
	ok, err = assetExists(openFile, "/assets")
	if err != nil || ok {
		t.Fatalf("assetExists(dir) = %v err=%v", ok, err)
	}
	ok, err = assetExists(openFile, "/missing.js")
	if err != nil || ok {
		t.Fatalf("assetExists(missing) = %v err=%v", ok, err)
	}

	_, embeddedOpen, err := resolveDashboardSource(filepath.Join(dir, "missing"))
	if err != nil {
		t.Fatalf("resolveDashboardSource(embed fallback): %v", err)
	}
	ok, err = assetExists(embeddedOpen, "/index.html")
	if err != nil || !ok {
		t.Fatalf("embedded assetExists(index) = %v err=%v", ok, err)
	}

	_, err = embeddedOpen("/definitely-not-real")
	if err == nil {
		t.Fatalf("expected embedded open to fail for missing file")
	}
	if !os.IsNotExist(err) && err != fs.ErrNotExist {
		t.Fatalf("expected not-exist error, got %v", err)
	}
}
