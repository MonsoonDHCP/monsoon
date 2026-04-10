package rest

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func NewSPADashboardHandler(distDir, basePath string, metricsPath string) (http.Handler, error) {
	if strings.TrimSpace(distDir) == "" {
		return nil, fmt.Errorf("dashboard dist directory is empty")
	}
	if strings.TrimSpace(basePath) == "" {
		basePath = "/"
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	basePath = path.Clean(basePath)

	indexPath := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return nil, fmt.Errorf("dashboard index not found at %s: %w", indexPath, err)
	}
	fs := http.FileServer(http.Dir(distDir))

	normalizedMetrics := metricsPath
	if normalizedMetrics == "" {
		normalizedMetrics = "/metrics"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == normalizedMetrics || strings.HasPrefix(r.URL.Path, normalizedMetrics+"/") {
			http.NotFound(w, r)
			return
		}

		requestPath := r.URL.Path
		if basePath != "/" {
			if !strings.HasPrefix(requestPath, basePath) {
				http.NotFound(w, r)
				return
			}
			requestPath = strings.TrimPrefix(requestPath, basePath)
			if requestPath == "" {
				requestPath = "/"
			}
		}

		cleanPath := path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
		candidate := filepath.Join(distDir, filepath.FromSlash(strings.TrimPrefix(cleanPath, "/")))
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			r2 := r.Clone(r.Context())
			r2.URL.Path = cleanPath
			fs.ServeHTTP(w, r2)
			return
		}

		http.ServeFile(w, r, indexPath)
	}), nil
}
