package rest

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monsoondhcp/monsoon/internal/auth"
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

func RecoveryMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("panic recovered: %v", rec)
					WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func LoggingMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			log.Printf("http %s %s from=%s dur=%s", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
		})
	}
}

func CORSMiddleware(origins []string) Middleware {
	allowAny := len(origins) == 0
	if len(origins) == 1 && origins[0] == "*" {
		allowAny = true
	}
	allow := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allow[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if allowAny && origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else if _, ok := allow[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
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
	var lim sync.Map
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			entry, _ := lim.LoadOrStore(host, newLimiter(rps))
			if !entry.(*tokenBucketLimiter).allow() {
				WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type requestIDKey struct{}

type identityContextKey struct{}

func AuthMiddleware(service *auth.Service, enforce bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if service == nil || !enforce {
				next.ServeHTTP(w, r)
				return
			}
			if isPublicAuthPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			identity, ok := resolveIdentity(r, service)
			if !ok {
				WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			ctx := context.WithValue(r.Context(), identityContextKey{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
