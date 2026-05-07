package tenant

import "context"

type ID string

const Default ID = "default"

type ctxKey struct{}

func WithID(ctx context.Context, id ID) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func FromContext(ctx context.Context) ID {
	id, _ := ctx.Value(ctxKey{}).(ID)
	if id == "" {
		return Default
	}
	return id
}
