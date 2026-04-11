package rest

import (
	"bytes"
	"crypto/tls"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monsoondhcp/monsoon/internal/audit"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/metrics"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestSecurityHeadersMiddlewareSetsBaselineHeaders(t *testing.T) {
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		SecurityHeadersMiddleware(),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatalf("expected content security policy header")
	}
	if got := rr.Header().Get("Permissions-Policy"); got == "" {
		t.Fatalf("expected permissions policy header")
	}
	if got := rr.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("expected no-referrer policy, got %q", got)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected nosniff, got %q", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("expected deny frame options, got %q", got)
	}
	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("did not expect hsts on plain http request, got %q", got)
	}
}

func TestSecurityHeadersMiddlewareSetsHSTSForHTTPSRequests(t *testing.T) {
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		SecurityHeadersMiddleware(),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	req.TLS = &tls.ConnectionState{}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatalf("expected hsts header for tls request")
	}
}

func TestSecurityHeadersMiddlewareHonorsForwardedHTTPS(t *testing.T) {
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		TrustedProxyHeadersMiddleware([]string{"192.0.2.0/24"}),
		SecurityHeadersMiddleware(),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatalf("expected hsts header when reverse proxy indicates https")
	}
}

func TestSecurityHeadersMiddlewareIgnoresForwardedHTTPSFromUntrustedClient(t *testing.T) {
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		TrustedProxyHeadersMiddleware([]string{"192.0.2.0/24"}),
		SecurityHeadersMiddleware(),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	req.RemoteAddr = "198.51.100.10:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("did not expect hsts from untrusted forwarded proto, got %q", got)
	}
}

func TestTrustedProxyHeadersMiddlewareFuncAppliesUpdatedAllowlist(t *testing.T) {
	trusted := []string{"192.0.2.0/24"}
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		TrustedProxyHeadersMiddlewareFunc(func() []string {
			return append([]string(nil), trusted...)
		}),
		SecurityHeadersMiddleware(),
		LoggingMiddleware(),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	req.RemoteAddr = "198.51.100.10:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-For", "203.0.113.20")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("did not expect forwarded https before reload, got %q", got)
	}

	trusted = []string{"198.51.100.0/24"}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	req.RemoteAddr = "198.51.100.10:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatalf("expected forwarded https to be trusted after reload")
	}
}

func TestCORSMiddlewareFuncAppliesUpdatedOrigins(t *testing.T) {
	origins := []string{"https://app.example"}
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		CORSMiddlewareFunc(func() []string {
			return append([]string(nil), origins...)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	req.Header.Set("Origin", "https://other.example")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("did not expect origin to be allowed before reload, got %q", got)
	}

	origins = []string{"https://other.example"}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	req.Header.Set("Origin", "https://other.example")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://other.example" {
		t.Fatalf("expected updated cors origin to be allowed, got %q", got)
	}
}

func TestAuthRateLimitMiddlewareLimitsSensitiveRoutesPerBucket(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"audit"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	reg := metrics.NewRegistry()
	auditLogger := audit.NewLogger(eng)
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		AuthRateLimitMiddleware(1, reg, auditLogger),
	)

	firstLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	firstLogin.RemoteAddr = "192.0.2.10:1234"
	firstRR := httptest.NewRecorder()
	handler.ServeHTTP(firstRR, firstLogin)
	if firstRR.Code != http.StatusNoContent {
		t.Fatalf("expected first login request to pass, got %d", firstRR.Code)
	}

	secondLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	secondLogin.RemoteAddr = "192.0.2.10:1234"
	secondRR := httptest.NewRecorder()
	handler.ServeHTTP(secondRR, secondLogin)
	if secondRR.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second login request to be rate limited, got %d", secondRR.Code)
	}

	bootstrapReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", nil)
	bootstrapReq.RemoteAddr = "192.0.2.10:1234"
	bootstrapRR := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRR, bootstrapReq)
	if bootstrapRR.Code != http.StatusNoContent {
		t.Fatalf("expected bootstrap bucket to be independent, got %d", bootstrapRR.Code)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	healthReq.RemoteAddr = "192.0.2.10:1234"
	healthRR := httptest.NewRecorder()
	handler.ServeHTTP(healthRR, healthReq)
	if healthRR.Code != http.StatusNoContent {
		t.Fatalf("expected non-auth route to bypass auth rate limit, got %d", healthRR.Code)
	}

	exported := reg.Export()
	if exported == "" || !strings.Contains(exported, `monsoon_auth_rate_limited_total{endpoint="login"} 1.000000`) {
		t.Fatalf("expected auth rate limit metric to be exported, got %q", exported)
	}
	if !strings.Contains(exported, `monsoon_security_events_total{event="auth_rate_limited",surface="login"} 1.000000`) {
		t.Fatalf("expected aggregated security rate-limit metric to be exported, got %q", exported)
	}

	entries, err := auditLogger.Query(firstLogin.Context(), audit.QueryFilter{
		Action: "security.auth.rate_limit",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one audit entry, got %d", len(entries))
	}
	if entries[0].ObjectID != "login" {
		t.Fatalf("expected login rate-limit audit entry, got %+v", entries[0])
	}
}

func TestAuthMiddlewareFuncAppliesUpdatedEnforcement(t *testing.T) {
	enforced := false
	service := auth.NewService(nil, auth.ServiceOptions{CookieName: "monsoon_session"})
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		AuthMiddlewareFunc(service, func() bool {
			return enforced
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subnets", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected auth-disabled request to pass, got %d", rr.Code)
	}

	enforced = true
	req = httptest.NewRequest(http.MethodGet, "/api/v1/subnets", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected auth-enforced request to fail, got %d", rr.Code)
	}
}

func TestCSRFMiddlewareRejectsCrossSiteSessionMutation(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"audit"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	reg := metrics.NewRegistry()
	auditLogger := audit.NewLogger(eng)
	service := auth.NewService(nil, auth.ServiceOptions{CookieName: "monsoon_session"})
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		CSRFMiddleware(service, reg, auditLogger),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Host = "monsoon.example"
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(&http.Cookie{Name: "monsoon_session", Value: "abc123"})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected csrf rejection, got %d", rr.Code)
	}
	exported := reg.Export()
	if !strings.Contains(exported, `monsoon_csrf_rejected_total{path="/api/v1/auth/logout"} 1.000000`) {
		t.Fatalf("expected csrf metric to be exported, got %q", exported)
	}
	if !strings.Contains(exported, `monsoon_security_events_total{event="csrf_rejected",surface="session"} 1.000000`) {
		t.Fatalf("expected aggregated csrf security metric to be exported, got %q", exported)
	}

	entries, err := auditLogger.Query(req.Context(), audit.QueryFilter{
		Action: "security.csrf.reject",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one csrf audit entry, got %d", len(entries))
	}
	if entries[0].ObjectID != "/api/v1/auth/logout" {
		t.Fatalf("expected csrf audit object id to be request path, got %+v", entries[0])
	}
}

func TestCSRFMiddlewareAllowsSameOriginSessionMutation(t *testing.T) {
	service := auth.NewService(nil, auth.ServiceOptions{CookieName: "monsoon_session"})
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		CSRFMiddleware(service, nil, nil),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Host = "monsoon.example"
	req.Header.Set("Origin", "https://monsoon.example")
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: "monsoon_session", Value: "abc123"})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected same-origin session request to pass, got %d", rr.Code)
	}
}

func TestCSRFMiddlewareAllowsBearerMutationWithoutSessionCookie(t *testing.T) {
	service := auth.NewService(nil, auth.ServiceOptions{CookieName: "monsoon_session"})
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		CSRFMiddleware(service, nil, nil),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subnets", nil)
	req.Host = "monsoon.example"
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Authorization", "Bearer test-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected bearer-style request without session cookie to pass csrf middleware, got %d", rr.Code)
	}
}

func TestLoggingMiddlewareLogsRequestMetadataAndErrorCode(t *testing.T) {
	var buf bytes.Buffer
	restoreLogs := captureStandardLogger(&buf)
	defer restoreLogs()

	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			WriteError(w, http.StatusForbidden, "forbidden", "nope")
		}),
		RequestIDMiddleware(),
		LoggingMiddleware(),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subnets", nil)
	req.RemoteAddr = "203.0.113.9:4321"
	req.Header.Set("User-Agent", "MonsoonTest/1.0")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden status, got %d", rr.Code)
	}

	logLine := buf.String()
	assertLogContains(t, logLine,
		`msg="http request"`,
		`method="POST"`,
		`path="/api/v1/subnets"`,
		`status=403`,
		`remote_ip="203.0.113.9"`,
		`user_agent="MonsoonTest/1.0"`,
		`error_code="forbidden"`,
		`request_id="`,
	)
}

func TestLoggingMiddlewareLogsForwardedIPAndIdentity(t *testing.T) {
	var buf bytes.Buffer
	restoreLogs := captureStandardLogger(&buf)
	defer restoreLogs()

	identityMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithIdentity(r.Context(), auth.Identity{
				Username: "alice",
				Role:     auth.DefaultRoleAdmin,
				AuthType: "session",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		RequestIDMiddleware(),
		TrustedProxyHeadersMiddleware([]string{"10.0.0.0/8"}),
		identityMiddleware,
		LoggingMiddleware(),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.2")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected success status, got %d", rr.Code)
	}

	logLine := buf.String()
	assertLogContains(t, logLine,
		`status=204`,
		`remote_ip="198.51.100.7"`,
		`actor="alice"`,
		`auth_type="session"`,
		`error_code=""`,
	)
}

func TestLoggingMiddlewareIgnoresForwardedIPFromUntrustedPeer(t *testing.T) {
	var buf bytes.Buffer
	restoreLogs := captureStandardLogger(&buf)
	defer restoreLogs()

	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		RequestIDMiddleware(),
		TrustedProxyHeadersMiddleware([]string{"10.0.0.0/8"}),
		LoggingMiddleware(),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.2")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected success status, got %d", rr.Code)
	}

	logLine := buf.String()
	assertLogContains(t, logLine, `remote_ip="203.0.113.5"`)
	if strings.Contains(logLine, `remote_ip="198.51.100.7"`) {
		t.Fatalf("did not expect untrusted forwarded ip to be logged, got %q", logLine)
	}
}

func captureStandardLogger(target *bytes.Buffer) func() {
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldPrefix := log.Prefix()
	log.SetOutput(target)
	log.SetFlags(0)
	log.SetPrefix("")
	return func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	}
}

func assertLogContains(t *testing.T, logLine string, want ...string) {
	t.Helper()
	for _, fragment := range want {
		if !strings.Contains(logLine, fragment) {
			t.Fatalf("expected log to contain %q, got %q", fragment, logLine)
		}
	}
}
