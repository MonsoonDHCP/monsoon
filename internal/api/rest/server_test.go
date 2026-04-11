package rest

import (
	"net/http"
	"strings"
	"testing"
)

func TestServerStartWithMissingTLSFilesFails(t *testing.T) {
	server := NewServer(
		"127.0.0.1:0",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		WithTLS("missing.crt", "missing.key"),
	)

	err := server.Start()
	if err == nil {
		t.Fatalf("expected tls startup to fail when cert files are missing")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "missing") && !strings.Contains(strings.ToLower(err.Error()), "open") {
		t.Fatalf("expected tls file error, got %v", err)
	}
}
