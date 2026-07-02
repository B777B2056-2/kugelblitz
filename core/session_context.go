package core

import "context"

// sessionIDKey is the context key for the current session ID.
type sessionIDKey struct{}

// WithSessionID stores the session ID in the context.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

// SessionIDFromContext retrieves the session ID from the context, or "" if not set.
func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(sessionIDKey{}).(string); ok {
		return v
	}
	return ""
}
