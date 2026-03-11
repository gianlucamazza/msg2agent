package agent

import "context"

type contextKey int

const (
	ctxKeyFrom contextKey = iota
	ctxKeyMethod
)

func MessageFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyFrom).(string)
	return v
}

func MessageMethod(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyMethod).(string)
	return v
}
