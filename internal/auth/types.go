package auth

import "time"

const (
	DefaultRoleAdmin    = "admin"
	DefaultRoleOperator = "operator"
	DefaultRoleViewer   = "viewer"
)

type User struct {
	Username     string    `json:"username"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Identity struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	AuthType string `json:"auth_type"`
	TokenID  string `json:"token_id,omitempty"`
}

type APIToken struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Role        string     `json:"role"`
	Prefix      string     `json:"prefix"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	Description string     `json:"description,omitempty"`
}

type tokenRecord struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Role        string     `json:"role"`
	Hash        string     `json:"hash"`
	Prefix      string     `json:"prefix"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	Description string     `json:"description,omitempty"`
}

func sanitizeRole(role string) string {
	switch role {
	case DefaultRoleAdmin, DefaultRoleOperator, DefaultRoleViewer:
		return role
	default:
		return DefaultRoleViewer
	}
}
