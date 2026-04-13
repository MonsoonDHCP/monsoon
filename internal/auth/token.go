package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/storage"
)

const (
	treeTokens = "api_tokens"
	// #nosec G101 -- bucket/tree identifier, not a credential.
	treeTokensByHash = "api_tokens_by_hash"
)

func (s *Service) CreateToken(ctx context.Context, name string, role string, expiresAt *time.Time, description string) (APIToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return APIToken{}, "", errors.New("token name is required")
	}
	rawToken, err := randomHex(32)
	if err != nil {
		return APIToken{}, "", err
	}
	tokenID, err := randomHex(12)
	if err != nil {
		return APIToken{}, "", err
	}
	hash := tokenHash(rawToken)
	now := time.Now().UTC()

	record := tokenRecord{
		ID:          tokenID,
		Name:        name,
		Role:        sanitizeRole(strings.TrimSpace(role)),
		Hash:        hash,
		Prefix:      rawToken[:8],
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		Description: strings.TrimSpace(description),
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return APIToken{}, "", err
	}
	if err := s.store.Tx(func(tx *storage.Tx) error {
		tx.Put(treeTokens, []byte(record.ID), raw)
		tx.Put(treeTokensByHash, []byte(hash), []byte(record.ID))
		return nil
	}); err != nil {
		return APIToken{}, "", err
	}

	return toPublicToken(record), "msn_" + rawToken, nil
}

func (s *Service) ListTokens(_ context.Context) ([]APIToken, error) {
	out := make([]APIToken, 0, 32)
	err := s.store.Iterate(treeTokens, nil, nil, func(_, v []byte) bool {
		var record tokenRecord
		if json.Unmarshal(v, &record) == nil {
			out = append(out, toPublicToken(record))
		}
		return true
	})
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	return out, nil
}

func (s *Service) RevokeToken(_ context.Context, tokenID string) error {
	raw, err := s.store.Get(treeTokens, []byte(strings.TrimSpace(tokenID)))
	if err != nil {
		return err
	}
	var record tokenRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return err
	}
	return s.store.Tx(func(tx *storage.Tx) error {
		tx.Delete(treeTokens, []byte(record.ID))
		tx.Delete(treeTokensByHash, []byte(record.Hash))
		return nil
	})
}

func (s *Service) AuthenticateBearer(ctx context.Context, token string) (Identity, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Identity{}, ErrInvalidCredentials
	}
	token = strings.TrimPrefix(token, "msn_")
	hash := tokenHash(token)
	tokenIDRaw, err := s.store.Get(treeTokensByHash, []byte(hash))
	if err != nil {
		return Identity{}, ErrInvalidCredentials
	}
	tokenID := string(tokenIDRaw)
	raw, err := s.store.Get(treeTokens, []byte(tokenID))
	if err != nil {
		return Identity{}, ErrInvalidCredentials
	}
	var record tokenRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return Identity{}, err
	}
	now := time.Now().UTC()
	if record.ExpiresAt != nil && now.After(*record.ExpiresAt) {
		return Identity{}, ErrInvalidCredentials
	}

	record.LastUsedAt = &now
	if updated, marshalErr := json.Marshal(record); marshalErr == nil {
		_ = s.store.Put(treeTokens, []byte(record.ID), updated)
	}

	return Identity{
		Username: "token:" + record.Name,
		Role:     record.Role,
		AuthType: "token",
		TokenID:  record.ID,
	}, nil
}

func toPublicToken(record tokenRecord) APIToken {
	return APIToken{
		ID:          record.ID,
		Name:        record.Name,
		Role:        record.Role,
		Prefix:      record.Prefix,
		CreatedAt:   record.CreatedAt,
		ExpiresAt:   record.ExpiresAt,
		LastUsedAt:  record.LastUsedAt,
		Description: record.Description,
	}
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("invalid byte length")
	}
	buf := make([]byte, n)
	if _, err := randRead(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
