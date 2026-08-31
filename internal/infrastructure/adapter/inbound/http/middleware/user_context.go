package middleware

import (
	"context"
	"errors"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

const (
	UserContextKey contextKey = "userId"
)

var (
	ErrNoUserInContext = errors.New("no authenticated user found in context")
)

func WithUser(ctx context.Context, userId model.UserId) context.Context {
	return context.WithValue(ctx, UserContextKey, userId)
}

func GetUserFromContext(ctx context.Context) (model.UserId, error) {
	if u, ok := ctx.Value(UserContextKey).(model.UserId); ok {
		return u, nil
	}
	return model.UserId{}, ErrNoUserInContext
}
