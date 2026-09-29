package domain

import "context"

type nativeHistoryRequirementKey struct{}

// WithNativeHistoryRequired is a narrowing request option, never an authority
// grant. Managed projects impose the same restriction even when it is omitted.
func WithNativeHistoryRequired(ctx context.Context) context.Context {
	return context.WithValue(ctx, nativeHistoryRequirementKey{}, true)
}

// NativeHistoryRequired reports whether the context carries a native-history
// narrowing request (see WithNativeHistoryRequired).
func NativeHistoryRequired(ctx context.Context) bool {
	required, _ := ctx.Value(nativeHistoryRequirementKey{}).(bool)
	return required
}
