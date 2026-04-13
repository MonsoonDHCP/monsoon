package rest

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/audit"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/metrics"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type bootstrapRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type createTokenRequest struct {
	Name           string `json:"name"`
	Role           string `json:"role"`
	ExpiresInHours int    `json:"expires_in_hours"`
	Description    string `json:"description"`
}

func registerAuthRoutes(mux *http.ServeMux, service *auth.Service, secureCookie func() bool, logger *audit.Logger, registry *metrics.Registry) {
	mux.HandleFunc("POST /api/v1/auth/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if !requireLocalAuthMode(w, service, registry, "bootstrap") {
			return
		}
		var payload bootstrapRequest
		if err := decodeJSONBody(r, &payload); err != nil {
			recordAuthRequestMetric(registry, "bootstrap", "invalid_payload")
			WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		username := strings.TrimSpace(payload.Username)
		if username == "" {
			username = "admin"
		}
		if strings.TrimSpace(payload.Password) == "" {
			recordAuthRequestMetric(registry, "bootstrap", "invalid_password")
			WriteError(w, http.StatusBadRequest, "invalid_password", "password is required")
			return
		}
		if err := service.BootstrapAdmin(r.Context(), username, payload.Password); err != nil {
			if err == auth.ErrBootstrapUnavailable {
				recordAuthRequestMetric(registry, "bootstrap", "unavailable")
				WriteError(w, http.StatusConflict, "bootstrap_unavailable", "admin bootstrap has already been completed")
				return
			}
			recordAuthRequestMetric(registry, "bootstrap", "error")
			WriteError(w, http.StatusInternalServerError, "bootstrap_failed", err.Error())
			return
		}
		recordAuthRequestMetric(registry, "bootstrap", "success")
		logAuditEntry(r, logger, audit.Entry{
			Actor:      username,
			Action:     "auth.bootstrap",
			ObjectType: "user",
			ObjectID:   username,
			Source:     "api",
		})
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ready", "username": username}, nil)
	})

	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if !requireLocalAuthMode(w, service, registry, "login") {
			return
		}
		var payload loginRequest
		if err := decodeJSONBody(r, &payload); err != nil {
			recordAuthRequestMetric(registry, "login", "invalid_payload")
			WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		identity, err := service.AuthenticateLocal(r.Context(), payload.Username, payload.Password)
		if err != nil {
			if writeLockedAuthError(w, err) {
				recordAuthRequestMetric(registry, "login", "locked")
				recordSecurityEventMetric(registry, "account_locked", "login")
				logSecurityAuditEntry(r, logger, audit.Entry{
					Actor:      strings.TrimSpace(payload.Username),
					Action:     "auth.login.locked",
					ObjectType: "user",
					ObjectID:   strings.TrimSpace(payload.Username),
					Meta: map[string]any{
						"endpoint": "login",
					},
				})
				return
			}
			recordAuthRequestMetric(registry, "login", "invalid_credentials")
			recordSecurityEventMetric(registry, "auth_failure", "login")
			logSecurityAuditEntry(r, logger, audit.Entry{
				Actor:      strings.TrimSpace(payload.Username),
				Action:     "auth.login.failed",
				ObjectType: "user",
				ObjectID:   strings.TrimSpace(payload.Username),
				Meta: map[string]any{
					"endpoint": "login",
					"reason":   "invalid_credentials",
				},
			})
			WriteError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
			return
		}
		sessionID, expiresAt, err := service.CreateSession(r.Context(), identity)
		if err != nil {
			recordAuthRequestMetric(registry, "login", "session_create_failed")
			WriteError(w, http.StatusInternalServerError, "session_create_failed", err.Error())
			return
		}
		recordAuthRequestMetric(registry, "login", "success")
		setSessionCookie(w, service.CookieName(), sessionID, expiresAt, authSecureCookie(secureCookie))
		logAuditEntry(r, logger, audit.Entry{
			Actor:      identity.Username,
			Action:     "auth.login",
			ObjectType: "session",
			ObjectID:   sessionID,
			Source:     "api",
		})
		WriteJSON(w, http.StatusOK, identity, nil)
	})

	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		actor := requestActor(r)
		if cookie, err := r.Cookie(service.CookieName()); err == nil {
			service.RevokeSession(r.Context(), cookie.Value)
		}
		recordAuthRequestMetric(registry, "logout", "success")
		// #nosec G124 -- Secure is intentionally environment-configurable for HTTP dev mode.
		http.SetCookie(w, &http.Cookie{
			Name:     service.CookieName(),
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   authSecureCookie(secureCookie),
			SameSite: http.SameSiteStrictMode,
		})
		logAuditEntry(r, logger, audit.Entry{
			Actor:      actor,
			Action:     "auth.logout",
			ObjectType: "session",
			ObjectID:   actor,
			Source:     "api",
		})
		WriteJSON(w, http.StatusOK, map[string]string{"status": "logged_out"}, nil)
	})

	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		identity, ok := IdentityFromContext(r.Context())
		if !ok {
			identity, ok = resolveIdentity(r, service)
		}
		if !ok {
			recordAuthRequestMetric(registry, "me", "unauthorized")
			WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		recordAuthRequestMetric(registry, "me", "success")
		WriteJSON(w, http.StatusOK, identity, nil)
	})

	mux.HandleFunc("POST /api/v1/auth/password", func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if !requireLocalAuthMode(w, service, registry, "password") {
			return
		}
		identity, ok := IdentityFromContext(r.Context())
		if !ok {
			recordAuthRequestMetric(registry, "password", "unauthorized")
			WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		var payload passwordChangeRequest
		if err := decodeJSONBody(r, &payload); err != nil {
			recordAuthRequestMetric(registry, "password", "invalid_payload")
			WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		if _, err := service.AuthenticateLocal(r.Context(), identity.Username, payload.CurrentPassword); err != nil {
			if writeLockedAuthError(w, err) {
				recordAuthRequestMetric(registry, "password", "locked")
				recordSecurityEventMetric(registry, "account_locked", "password")
				logSecurityAuditEntry(r, logger, audit.Entry{
					Actor:      identity.Username,
					Action:     "auth.password.locked",
					ObjectType: "user",
					ObjectID:   identity.Username,
					Meta: map[string]any{
						"endpoint": "password",
					},
				})
				return
			}
			recordAuthRequestMetric(registry, "password", "invalid_credentials")
			recordSecurityEventMetric(registry, "auth_failure", "password")
			logSecurityAuditEntry(r, logger, audit.Entry{
				Actor:      identity.Username,
				Action:     "auth.password.verify_failed",
				ObjectType: "user",
				ObjectID:   identity.Username,
				Meta: map[string]any{
					"endpoint": "password",
					"reason":   "invalid_credentials",
				},
			})
			WriteError(w, http.StatusUnauthorized, "invalid_credentials", "current password is invalid")
			return
		}
		if err := service.SetPassword(r.Context(), identity.Username, payload.NewPassword); err != nil {
			recordAuthRequestMetric(registry, "password", "password_update_failed")
			WriteError(w, http.StatusBadRequest, "password_update_failed", err.Error())
			return
		}
		service.RevokeSessionsForUser(r.Context(), identity.Username)
		if identity.AuthType == "session" {
			sessionID, expiresAt, err := service.CreateSession(r.Context(), identity)
			if err != nil {
				recordAuthRequestMetric(registry, "password", "session_create_failed")
				WriteError(w, http.StatusInternalServerError, "session_create_failed", err.Error())
				return
			}
			setSessionCookie(w, service.CookieName(), sessionID, expiresAt, authSecureCookie(secureCookie))
		}
		recordAuthRequestMetric(registry, "password", "success")
		logAuditEntry(r, logger, audit.Entry{
			Actor:      identity.Username,
			Action:     "auth.password.update",
			ObjectType: "user",
			ObjectID:   identity.Username,
			Source:     "api",
		})
		WriteJSON(w, http.StatusOK, map[string]string{"status": "updated"}, nil)
	})

	mux.HandleFunc("GET /api/v1/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if !requireRoleWithService(w, r, service, auth.DefaultRoleAdmin) {
			return
		}
		tokens, err := service.ListTokens(r.Context())
		if err != nil {
			recordAuthRequestMetric(registry, "tokens.list", "error")
			WriteError(w, http.StatusInternalServerError, "token_list_failed", err.Error())
			return
		}
		recordAuthRequestMetric(registry, "tokens.list", "success")
		WriteJSON(w, http.StatusOK, tokens, map[string]any{"total": len(tokens)})
	})

	mux.HandleFunc("POST /api/v1/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if !requireRoleWithService(w, r, service, auth.DefaultRoleAdmin) {
			return
		}
		var payload createTokenRequest
		if err := decodeJSONBody(r, &payload); err != nil {
			recordAuthRequestMetric(registry, "tokens.create", "invalid_payload")
			WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		var expiresAt *time.Time
		if payload.ExpiresInHours > 0 {
			v := time.Now().UTC().Add(time.Duration(payload.ExpiresInHours) * time.Hour)
			expiresAt = &v
		}
		token, secret, err := service.CreateToken(r.Context(), payload.Name, payload.Role, expiresAt, payload.Description)
		if err != nil {
			recordAuthRequestMetric(registry, "tokens.create", "create_failed")
			WriteError(w, http.StatusBadRequest, "token_create_failed", err.Error())
			return
		}
		recordAuthRequestMetric(registry, "tokens.create", "success")
		logAuditEntry(r, logger, audit.Entry{
			Actor:      requestActor(r),
			Action:     "auth.token.create",
			ObjectType: "api_token",
			ObjectID:   token.ID,
			Source:     "api",
			After: map[string]any{
				"name": token.Name,
				"role": token.Role,
			},
		})
		WriteJSON(w, http.StatusOK, map[string]any{"token": token, "secret": secret}, nil)
	})

	mux.HandleFunc("DELETE /api/v1/auth/tokens/{id}", func(w http.ResponseWriter, r *http.Request) {
		setNoStore(w)
		if !requireRoleWithService(w, r, service, auth.DefaultRoleAdmin) {
			return
		}
		tokenID := strings.TrimSpace(r.PathValue("id"))
		if tokenID == "" {
			recordAuthRequestMetric(registry, "tokens.revoke", "missing_id")
			WriteError(w, http.StatusBadRequest, "missing_id", "token id is required")
			return
		}
		if err := service.RevokeToken(r.Context(), tokenID); err != nil {
			recordAuthRequestMetric(registry, "tokens.revoke", "not_found")
			WriteError(w, http.StatusNotFound, "token_not_found", "token not found")
			return
		}
		recordAuthRequestMetric(registry, "tokens.revoke", "success")
		logAuditEntry(r, logger, audit.Entry{
			Actor:      requestActor(r),
			Action:     "auth.token.revoke",
			ObjectType: "api_token",
			ObjectID:   tokenID,
			Source:     "api",
		})
		WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked", "id": tokenID}, nil)
	})
}

func setNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func authSecureCookie(value func() bool) bool {
	if value == nil {
		return false
	}
	return value()
}

func setSessionCookie(w http.ResponseWriter, name, sessionID string, expiresAt time.Time, secureCookie bool) {
	// #nosec G124 -- Secure is intentionally environment-configurable for HTTP dev mode.
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookie,
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresAt,
	})
}

func recordAuthRequestMetric(registry *metrics.Registry, endpoint string, outcome string) {
	if registry == nil {
		return
	}
	registry.IncCounter("monsoon_auth_requests_total", map[string]string{
		"endpoint": endpoint,
		"outcome":  outcome,
	}, 1)
}

func recordSecurityEventMetric(registry *metrics.Registry, event string, surface string) {
	if registry == nil {
		return
	}
	registry.IncCounter("monsoon_security_events_total", map[string]string{
		"event":   event,
		"surface": surface,
	}, 1)
}

func writeLockedAuthError(w http.ResponseWriter, err error) bool {
	var lockedErr auth.AccountLockedError
	if !errors.As(err, &lockedErr) {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(lockedErr.RetryAfter(time.Now().UTC())))
	WriteError(w, http.StatusTooManyRequests, "account_locked", "account is temporarily locked")
	return true
}

func requireLocalAuthMode(w http.ResponseWriter, service *auth.Service, registry *metrics.Registry, endpoint string) bool {
	if service == nil || service.SupportsLocalAuth() {
		return true
	}
	recordAuthRequestMetric(registry, endpoint, "unsupported_mode")
	WriteError(w, http.StatusNotImplemented, "auth_mode_unsupported", "configured auth mode does not support local username/password operations in this build")
	return false
}

func requireRoleWithService(w http.ResponseWriter, r *http.Request, service *auth.Service, requiredRole string) bool {
	identity, ok := IdentityFromContext(r.Context())
	if !ok {
		identity, ok = resolveIdentity(r, service)
	}
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return false
	}
	if !auth.HasRole(requiredRole, identity.Role) {
		WriteError(w, http.StatusForbidden, "forbidden", "insufficient role")
		return false
	}
	return true
}
