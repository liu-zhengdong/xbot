package cliops

import "context"

// sourceCallIDKey carries correlation with a real source-local Job. The value
// alone never establishes caller identity or an approval context.
type sourceCallIDKey struct{}

func optionalSourceCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sourceCallIDKey{}, id)
}

func SourceCallID(ctx context.Context) string {
	id, _ := ctx.Value(sourceCallIDKey{}).(string)
	return id
}
