package rest

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monsoondhcp/monsoon/internal/audit"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/metrics"
)

type Middleware func(http.Handler) http.Handler

func Chain(next http.Handler, m ...Middleware) http.Handler {
	if len(m) == 0 {
		return next
	}
	wrapped := next
	for i := len(m) - 1; i >= 0; i-- {
		wrapped = m[i](wrapped)
	}
	return wrapped
}

func RequestIDMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := strconv.FormatInt(time.Now().UnixNano(), 36)
			w.Header().Set("X-Request-ID", requestID)
			ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequestIDFromContext(ctx context.Context) (string, bool) {
	raw := ctx.Value(requestIDKey{})
	requestID, ok := raw.(string)
	return requestID, ok && requestID != ""
}

func TrustedProxyHeadersMiddleware(trusted []string) Middleware {
	return TrustedProxyHeadersMiddlewareFunc(func() []string {
		return append([]string(nil), trusted...)
	})
}

func TrustedProxyHeadersMiddlewareFunc(trusted func() []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			currentTrusted := []string(nil)
			if trusted != nil {
				currentTrusted = trusted()
			}
			matcher := newTrustedProxyMatcher(currentTrusted)
			ctx := context.WithValue(r.Context(), trustedProxyContextKey{}, matcher.isTrusted(r.RemoteAddr))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RecoveryMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					requestID, _ := RequestIDFromContext(r.Context())
					log.Printf("level=error msg=\"panic recovered\" request_id=%q method=%q path=%q remote_ip=%q panic=%q", requestID, r.Method, r.URL.Path, clientIP(r), rec)
					WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func SecurityHeadersMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headers := w.Header()
			headers.Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' ws: wss:")
			headers.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
			headers.Set("Referrer-Policy", "no-referrer")
			headers.Set("X-Content-Type-Options", "nosniff")
			headers.Set("X-Frame-Options", "DENY")
			if isHTTPSRequest(r) {
				headers.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func LoggingMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			requestID, _ := RequestIDFromContext(r.Context())
			actor := ""
			authType := ""
			if identity, ok := IdentityFromContext(r.Context()); ok {
				actor = identity.Username
				authType = identity.AuthType
			}

			log.Printf(
				"level=info msg=\"http request\" request_id=%q method=%q path=%q status=%d bytes=%d dur_ms=%d remote_ip=%q user_agent=%q actor=%q auth_type=%q error_code=%q",
				requestID,
				r.Method,
				r.URL.Path,
				rec.status,
				rec.bytes,
				time.Since(start).Milliseconds(),
				clientIP(r),
				r.UserAgent(),
				actor,
				authType,
				rec.errorCode,
			)
		})
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status    int
	bytes     int
	errorCode string
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(body)
	r.bytes += n
	return n, err
}

func (r *responseRecorder) SetErrorCode(code string) {
	r.errorCode = code
}

func CORSMiddleware(origins []string) Middleware {
	return CORSMiddlewareFunc(func() []string {
		return append([]string(nil), origins...)
	})
}

func CORSMiddlewareFunc(origins func() []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			currentOrigins := []string(nil)
			if origins != nil {
				currentOrigins = origins()
			}
			allowAny := len(currentOrigins) == 1 && currentOrigins[0] == "*"
			allow := make(map[string]struct{}, len(currentOrigins))
			for _, item := range currentOrigins {
				allow[item] = struct{}{}
			}
			origin := r.Header.Get("Origin")
			if allowAny && origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			} else if _, ok := allow[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func CSRFMiddleware(service *auth.Service, registry *metrics.Registry, logger *audit.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if service == nil || !isUnsafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			if _, err := r.Cookie(service.CookieName()); err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if isTrustedCSRFRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			if registry != nil {
				registry.IncCounter("monsoon_csrf_rejected_total", map[string]string{"path": r.URL.Path}, 1)
			}
			recordSecurityEventMetric(registry, "csrf_rejected", "session")
			logSecurityAuditEntry(r, logger, audit.Entry{
				Actor:      requestActor(r),
				Action:     "security.csrf.reject",
				ObjectType: "http_request",
				ObjectID:   r.URL.Path,
				Meta: map[string]any{
					"reason": "cross_site_session_request",
				},
			})
			WriteError(w, http.StatusForbidden, "csrf_rejected", "cross-site session request rejected")
		})
	}
}

type tokenBucketLimiter struct {
	mu       sync.Mutex
	last     time.Time
	tokens   float64
	rate     float64
	capacity float64
}

func newLimiter(rate int) *tokenBucketLimiter {
	r := float64(rate)
	if r <= 0 {
		r = 1
	}
	return &tokenBucketLimiter{last: time.Now(), tokens: r, rate: r, capacity: r}
}

func (l *tokenBucketLimiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(l.last).Seconds()
	l.last = now
	l.tokens += elapsed * l.rate
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}
	if l.tokens < 1 {
		return false
	}
	l.tokens -= 1
	return true
}

func RateLimitMiddleware(rps int) Middleware {
	return RateLimitMiddlewareFunc(func() int {
		return rps
	})
}

func RateLimitMiddlewareFunc(rps func() int) Middleware {
	var lim sync.Map
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			currentRPS := 1
			if rps != nil {
				currentRPS = rps()
			}
			limiterKey := host + "|" + strconv.Itoa(currentRPS)
			entry, _ := lim.LoadOrStore(limiterKey, newLimiter(currentRPS))
			if !entry.(*tokenBucketLimiter).allow() {
				WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func AuthRateLimitMiddleware(rps int, registry *metrics.Registry, logger *audit.Logger) Middleware {
	return AuthRateLimitMiddlewareFunc(func() int {
		return rps
	}, registry, logger)
}

func AuthRateLimitMiddlewareFunc(rps func() int, registry *metrics.Registry, logger *audit.Logger) Middleware {
	var lim sync.Map
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := authRateLimitKey(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			currentRPS := 1
			if rps != nil {
				currentRPS = rps()
			}
			limiterKey := host + "|" + key + "|" + strconv.Itoa(currentRPS)
			entry, _ := lim.LoadOrStore(limiterKey, newLimiter(currentRPS))
			if !entry.(*tokenBucketLimiter).allow() {
				if registry != nil {
					registry.IncCounter("monsoon_auth_rate_limited_total", map[string]string{"endpoint": key}, 1)
				}
				recordSecurityEventMetric(registry, "auth_rate_limited", key)
				logSecurityAuditEntry(r, logger, audit.Entry{
					Actor:      requestActor(r),
					Action:     "security.auth.rate_limit",
					ObjectType: "auth_endpoint",
					ObjectID:   key,
					Meta: map[string]any{
						"endpoint": key,
					},
				})
				WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many authentication requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type requestIDKey struct{}

type identityContextKey struct{}
type authEnforcedContextKey struct{}
type trustedProxyContextKey struct{}

func AuthMiddleware(service *auth.Service, enforce bool) Middleware {
	return AuthMiddlewareFunc(service, func() bool {
		return enforce
	})
}

func AuthMiddlewareFunc(service *auth.Service, enforce func() bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			enforced := false
			if enforce != nil {
				enforced = enforce()
			}
			if service == nil || !enforced {
				ctx := WithAuthEnforcement(r.Context(), false)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if isPublicAuthPath(r.URL.Path) {
				ctx := WithAuthEnforcement(r.Context(), true)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			identity, ok := resolveIdentity(r, service)
			if !ok {
				WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			ctx := WithAuthEnforcement(r.Context(), true)
			ctx = context.WithValue(ctx, identityContextKey{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func WithAuthEnforcement(ctx context.Context, enforced bool) context.Context {
	return context.WithValue(ctx, authEnforcedContextKey{}, enforced)
}

func AuthEnforcedFromContext(ctx context.Context) bool {
	raw := ctx.Value(authEnforcedContextKey{})
	enforced, ok := raw.(bool)
	return ok && enforced
}

func WithIdentity(ctx context.Context, identity auth.Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

func IdentityFromContext(ctx context.Context) (auth.Identity, bool) {
	raw := ctx.Value(identityContextKey{})
	if raw == nil {
		return auth.Identity{}, false
	}
	identity, ok := raw.(auth.Identity)
	return identity, ok
}

func resolveIdentity(r *http.Request, service *auth.Service) (auth.Identity, bool) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token := strings.TrimSpace(authHeader[len("Bearer "):])
		identity, err := service.AuthenticateBearer(r.Context(), token)
		if err == nil {
			return identity, true
		}
	}
	cookie, err := r.Cookie(service.CookieName())
	if err == nil {
		identity, sessionErr := service.ValidateSession(r.Context(), cookie.Value)
		if sessionErr == nil {
			return identity, true
		}
		if !errors.Is(sessionErr, auth.ErrSessionNotFound) {
			return auth.Identity{}, false
		}
	}
	return auth.Identity{}, false
}

func isPublicAuthPath(path string) bool {
	switch path {
	case "/api/v1/system/health", "/api/v1/auth/login", "/api/v1/auth/bootstrap":
		return true
	default:
		return false
	}
}

func isHTTPSRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !forwardedHeadersTrusted(r) {
		return false
	}
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if forwarded == "" {
		return false
	}
	proto := strings.TrimSpace(strings.Split(forwarded, ",")[0])
	return strings.EqualFold(proto, "https")
}

func authRateLimitKey(r *http.Request) (string, bool) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/bootstrap":
		return "bootstrap", true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login":
		return "login", true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/logout":
		return "logout", true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/password":
		return "password", true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/tokens":
		return "tokens.create", true
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/auth/tokens/"):
		return "tokens.revoke", true
	default:
		return "", false
	}
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isTrustedCSRFRequest(r *http.Request) bool {
	if !isCrossSiteFetch(r) {
		return true
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}

	if !strings.EqualFold(parsed.Host, requestHost(r)) {
		return false
	}

	expectedScheme := "http"
	if isHTTPSRequest(r) {
		expectedScheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, expectedScheme)
}

func isCrossSiteFetch(r *http.Request) bool {
	site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	switch site {
	case "", "none", "same-origin", "same-site":
	default:
		return true
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return true
	}
	if !strings.EqualFold(parsed.Host, requestHost(r)) {
		return true
	}
	expectedScheme := "http"
	if isHTTPSRequest(r) {
		expectedScheme = "https"
	}
	return !strings.EqualFold(parsed.Scheme, expectedScheme)
}

func requestHost(r *http.Request) string {
	if forwardedHeadersTrusted(r) {
		forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
		if forwardedHost != "" {
			return strings.TrimSpace(strings.Split(forwardedHost, ",")[0])
		}
	}
	return r.Host
}

func clientIP(r *http.Request) string {
	if forwardedHeadersTrusted(r) {
		forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
		if forwardedFor != "" {
			return strings.TrimSpace(strings.Split(forwardedFor, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func forwardedHeadersTrusted(r *http.Request) bool {
	raw := r.Context().Value(trustedProxyContextKey{})
	trusted, ok := raw.(bool)
	return ok && trusted
}

type trustedProxyMatcher struct {
	addrs    []netip.Addr
	prefixes []netip.Prefix
}

func newTrustedProxyMatcher(values []string) trustedProxyMatcher {
	matcher := trustedProxyMatcher{
		addrs:    make([]netip.Addr, 0, len(values)),
		prefixes: make([]netip.Prefix, 0, len(values)),
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if addr, err := netip.ParseAddr(value); err == nil {
			matcher.addrs = append(matcher.addrs, addr)
			continue
		}
		if prefix, err := netip.ParsePrefix(value); err == nil {
			matcher.prefixes = append(matcher.prefixes, prefix.Masked())
		}
	}
	return matcher
}

func (m trustedProxyMatcher) isTrusted(remoteAddr string) bool {
	host := remoteAddr
	if parsedHost, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, allowed := range m.addrs {
		if allowed == addr {
			return true
		}
	}
	for _, prefix := range m.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
