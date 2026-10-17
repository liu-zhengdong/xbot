package remote

import (
	"context"
	"time"

	"xbot/internal/gateway"
)

// Run owns one poll loop per process. State locks serialize concurrent receivers.
func Run(ctx context.Context, root, token string, logf func(string, ...any)) {
	registered := map[string]bool{}
	backoff := time.Second
	for ctx.Err() == nil {
		cfg, err := Load(root)
		if err == nil && cfg.Gateway == "" {
			if !pause(ctx, 30*time.Second) {
				return
			}
			continue
		}
		if err == nil {
			err = func() error {
				priv, self, _, err := Identity(root)
				if err != nil {
					return err
				}
				client := &gateway.Client{Base: cfg.Gateway, Key: priv}
				if !registered[cfg.Gateway] {
					if err := client.Register(ctx, token); err != nil {
						return err
					}
					registered[cfg.Gateway] = true
					if logf != nil {
						logf("registered")
					}
				}
				envs, err := client.Poll(ctx, 25*time.Second)
				if err != nil {
					return err
				}
				ack, receiveErr := Receive(root, self, envs, time.Now(), logf)
				if len(ack) > 0 {
					if err := client.Ack(ctx, ack); err != nil {
						return err
					}
					if logf != nil {
						for _, id := range ack {
							logf("acked %s", id)
						}
					}
				}
				return receiveErr
			}()
		}
		if err == nil {
			backoff = time.Second
			continue
		}
		// Remote error bodies may contain secrets; never include them in logs.
		if logf != nil && ctx.Err() == nil {
			logf("retrying after gateway or local state failure")
		}
		if !pause(ctx, backoff) {
			return
		}
		backoff = min(2*backoff, time.Minute)
	}
}

func pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
