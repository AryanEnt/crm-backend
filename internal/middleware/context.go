package middleware

import "context"

func withValue(ctx context.Context, key contextKey, value string) context.Context {
	return context.WithValue(ctx, key, value)
}

// RequestIDFromContext returns the request ID if present.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(RequestIDKey).(string); ok {
		return v
	}
	return ""
}
