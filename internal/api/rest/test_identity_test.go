package rest

import (
	"context"

	"github.com/monsoondhcp/monsoon/internal/auth"
)

func withTestIdentity(ctx context.Context, identity auth.Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}
