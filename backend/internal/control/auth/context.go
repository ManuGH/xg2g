package auth

import (
	"context"
)

type contextKey struct{}

// WithPrincipal adds the principal to the context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// PrincipalFromContext retrieves the principal from the context.
func PrincipalFromContext(ctx context.Context) *Principal {
	val := ctx.Value(contextKey{})
	if p, ok := val.(*Principal); ok {
		return p
	}
	return nil
}
