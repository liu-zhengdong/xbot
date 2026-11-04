package desktop

import "context"

var physicalDesktop = make(chan struct{}, 1)

type sequenceKey struct{}

// Sequence holds the physical desktop across a caller's complete action sequence.
// Perform shares this lock with local Bot calls. The marker stays in this context.
func Sequence(ctx context.Context, run func(context.Context) error) error {
	if held, _ := ctx.Value(sequenceKey{}).(bool); held {
		return run(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case physicalDesktop <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-physicalDesktop }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return run(context.WithValue(ctx, sequenceKey{}, true))
}
