package auth

import (
	"context"
	"errors"
)

type contextKey string

const userContextKey contextKey = "user"

// setUserContext adds a user to the request context
func setUserContext(ctx context.Context, user *AuthUser) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// GetUserFromContext retrieves the user from the request context
func GetUserFromContext(ctx context.Context) (*AuthUser, error) {
	user, ok := ctx.Value(userContextKey).(*AuthUser)
	if !ok {
		return nil, errors.New("user not found in context")
	}
	return user, nil
}
