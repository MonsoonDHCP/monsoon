package rest

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/monsoondhcp/monsoon/internal/dashboard"
)

func NewSPADashboardHandler(distDir, basePath string, metricsPath string) (http.Handler, error) {
	if strings.TrimSpace(basePath) == "" {
		basePath = "/"
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	basePath = path.Clean(basePath)

	staticHandler, openFile, err := resolveDashboardSource(distDir)
	if err != nil {
		return nil, err
	}

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
		if ok, err := assetExists(openFile, cleanPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if ok {
			r2 := r.Clone(r.Context())
			r2.URL.Path = cleanPath
			staticHandler.ServeHTTP(w, r2)
			return
		}

		indexBody, err := openFile("/index.html")
		if err != nil {
			http.Error(w, "dashboard index not found", http.StatusInternalServerError)
			return
		}
		defer indexBody.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = io.Copy(w, indexBody)
	}), nil
}

func resolveDashboardSource(distDir string) (http.Handler, func(string) (fs.File, error), error) {
	if strings.TrimSpace(distDir) != "" {
		indexPath := filepath.Join(distDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			dirFS := os.DirFS(distDir)
			return http.FileServer(http.Dir(distDir)), func(name string) (fs.File, error) {
				return dirFS.Open(strings.TrimPrefix(name, "/"))
			}, nil
		}
	}

	embeddedFS, err := dashboard.FS()
	if err != nil {
		return nil, nil, fmt.Errorf("embedded dashboard unavailable: %w", err)
	}
	if _, err := embeddedFS.Open("index.html"); err != nil {
		return nil, nil, fmt.Errorf("embedded dashboard index not found: %w", err)
	}
	return http.FileServer(http.FS(embeddedFS)), func(name string) (fs.File, error) {
		return embeddedFS.Open(strings.TrimPrefix(name, "/"))
	}, nil
}

func assetExists(openFile func(string) (fs.File, error), cleanPath string) (bool, error) {
	if cleanPath == "/" {
		return false, nil
	}
	file, err := openFile(cleanPath)
	if err != nil {
		if os.IsNotExist(err) || err == fs.ErrNotExist {
			return false, nil
		}
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}
