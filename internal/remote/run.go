package remote

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"xbot/internal/gateway"
)

// Run owns one poll loop per process. State locks serialize concurrent receivers.
func Run(ctx context.Context, root, token string, logf func(string, ...any)) {
	rememberRunToken(root, token)
	defer forgetRunToken(root)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); runOutbox(ctx, root, logf) }()
	ready, dirDone := make(chan *gateway.Client, 1), make(chan struct{})
	go func() { defer close(dirDone); runDirectories(ctx, root, ready, logf) }()
	defer func() { cancel(); <-done; <-dirDone }()
	registered := map[string]bool{}
	var lastDirectoryGateway string
	backoff := time.Second
	for ctx.Err() == nil {
		cfg, err := Load(root)
		if err == nil && cfg.Gateway == "" && len(cfg.Devices) == 0 {
			var live bool
			live, err = hasLiveInvite(root, time.Now())
			if err == nil && !live && !pairingActive(root) {
				if !pause(ctx, root, 5*time.Second) {
					return
				}
				continue
			}
		}
		if err == nil {
			err = func() error {
				priv, self, _, err := Identity(root)
				if err != nil {
					return err
				}
				client := &gateway.Client{Base: cfg.GatewayURL(), Key: priv}
				if !registered[cfg.GatewayURL()] {
					err := client.Register(ctx, cfg.GatewayToken(token))
					var he *gateway.HTTPError
					switch {
					case err == nil:
						registered[cfg.GatewayURL()] = true
						if logf != nil {
							logf("registered")
						}
					case errors.As(err, &he) && (he.StatusCode == 401 || he.StatusCode == 403):
						// Not admitted (yet). A short-code joiner is a pending install
						// that may still poll its owner's pairing replies; retry the
						// registration on the next pass, which works once A admits it.
					default:
						return err
					}
				}
				notifyDirectory(ready, &lastDirectoryGateway, client)
				envs, err := client.Poll(ctx, 25*time.Second)
				if err != nil {
					return err
				}
				ack, bounces, receiveErr := Receive(root, self, envs, time.Now(), logf)
				for _, env := range bounces {
					if err := client.Send(ctx, env); retryable(err) {
						err = enqueue(root, outboxItem{Env: env, Created: time.Now(), Attempts: 1, Next: time.Now().Add(5 * time.Second)})
						if err != nil && logf != nil {
							logf("cannot queue bounce %s", env.ID)
						}
					}
				}
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
		if !pause(ctx, root, backoff) {
			return
		}
		backoff = min(2*backoff, time.Minute)
	}
}

// runWake cuts a poll-loop pause short after the gateway is set or a short-code
// claim, so a joiner starts polling within its 20-second pairing window.
// One channel per data root, so roots sharing a process (tests) do not steal
// each other's wake-ups.
var runWakes sync.Map // root -> chan struct{}

func wakeChan(root string) chan struct{} {
	ch, _ := runWakes.LoadOrStore(filepath.Clean(root), make(chan struct{}, 1))
	return ch.(chan struct{})
}

// dirWakes asks runDirectories to push now, e.g. right after a pairing, so the
// new device shows this computer's bots (and as online) within seconds.
var dirWakes sync.Map // root -> chan struct{}

func dirWake(root string) chan struct{} {
	ch, _ := dirWakes.LoadOrStore(filepath.Clean(root), make(chan struct{}, 1))
	return ch.(chan struct{})
}

func wakeDirectories(root string) {
	select {
	case dirWake(root) <- struct{}{}:
	default:
	}
}

func wakeRun(root string) {
	select {
	case wakeChan(root) <- struct{}{}:
	default:
	}
}

func pause(ctx context.Context, root string, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	case <-wakeChan(root):
		return true
	}
}

// notifyDirectory has one sender and a capacity-one channel. Replace pending
// work so a busy directory worker picks up the latest active gateway.
func notifyDirectory(ready chan *gateway.Client, lastURL *string, client *gateway.Client) {
	if *lastURL == client.Base {
		return
	}
	select {
	case <-ready:
	default:
	}
	ready <- client // the sole sender has just ensured there is room
	*lastURL = client.Base
}
