package rest

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/audit"
	"github.com/monsoondhcp/monsoon/internal/auth"
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

func registerAuthRoutes(mux *http.ServeMux, service *auth.Service, secureCookie bool, logger *audit.Logger) {
	mux.HandleFunc("POST /api/v1/auth/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		var payload bootstrapRequest
		if err := decodeJSONBody(r, &payload); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		username := strings.TrimSpace(payload.Username)
		if username == "" {
			username = "admin"
		}
		if strings.TrimSpace(payload.Password) == "" {
			WriteError(w, http.StatusBadRequest, "invalid_password", "password is required")
			return
		}
		if err := service.EnsureAdmin(r.Context(), username, ""); err != nil {
			WriteError(w, http.StatusInternalServerError, "bootstrap_failed", err.Error())
			return
		}
		if err := service.SetPassword(r.Context(), username, payload.Password); err != nil {
			WriteError(w, http.StatusInternalServerError, "bootstrap_failed", err.Error())
			return
		}
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
		var payload loginRequest
		if err := decodeJSONBody(r, &payload); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		identity, err := service.AuthenticateLocal(r.Context(), payload.Username, payload.Password)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
			return
		}
		sessionID, expiresAt, err := service.CreateSession(r.Context(), identity)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "session_create_failed", err.Error())
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     service.CookieName(),
			Value:    sessionID,
			Path:     "/",
			HttpOnly: true,
			Secure:   secureCookie,
			SameSite: http.SameSiteStrictMode,
			Expires:  expiresAt,
		})
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
		actor := requestActor(r)
		if cookie, err := r.Cookie(service.CookieName()); err == nil {
			service.RevokeSession(r.Context(), cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:     service.CookieName(),
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   secureCookie,
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
		identity, ok := IdentityFromContext(r.Context())
		if !ok {
			identity, ok = resolveIdentity(r, service)
		}
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		WriteJSON(w, http.StatusOK, identity, nil)
	})

	mux.HandleFunc("POST /api/v1/auth/password", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := IdentityFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		var payload passwordChangeRequest
		if err := decodeJSONBody(r, &payload); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		if _, err := service.AuthenticateLocal(r.Context(), identity.Username, payload.CurrentPassword); err != nil {
			WriteError(w, http.StatusUnauthorized, "invalid_credentials", "current password is invalid")
			return
		}
		if err := service.SetPassword(r.Context(), identity.Username, payload.NewPassword); err != nil {
			WriteError(w, http.StatusBadRequest, "password_update_failed", err.Error())
			return
		}
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
		if !requireRoleWithService(w, r, service, auth.DefaultRoleAdmin) {
			return
		}
		tokens, err := service.ListTokens(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "token_list_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, tokens, map[string]any{"total": len(tokens)})
	})

	mux.HandleFunc("POST /api/v1/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		if !requireRoleWithService(w, r, service, auth.DefaultRoleAdmin) {
			return
		}
		var payload createTokenRequest
		if err := decodeJSONBody(r, &payload); err != nil {
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
			WriteError(w, http.StatusBadRequest, "token_create_failed", err.Error())
			return
		}
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
		if !requireRoleWithService(w, r, service, auth.DefaultRoleAdmin) {
			return
		}
		tokenID := strings.TrimSpace(r.PathValue("id"))
		if tokenID == "" {
			WriteError(w, http.StatusBadRequest, "missing_id", "token id is required")
			return
		}
		if err := service.RevokeToken(context.Background(), tokenID); err != nil {
			WriteError(w, http.StatusNotFound, "token_not_found", "token not found")
			return
		}
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

func requireRole(w http.ResponseWriter, r *http.Request, requiredRole string) bool {
	identity, ok := IdentityFromContext(r.Context())
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
