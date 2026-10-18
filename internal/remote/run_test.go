package remote

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/gateway"
)

func TestNotifyDirectory(t *testing.T) {
	for _, consume := range []bool{true, false} {
		name := "latest wins"
		if consume {
			name = "gateway changes"
		}
		t.Run(name, func(t *testing.T) {
			ready := make(chan *gateway.Client, 1)
			var lastURL string
			done := make(chan struct{})
			stop := make(chan struct{})
			defer close(stop)
			a, b := &gateway.Client{Base: "A"}, &gateway.Client{Base: "B"}
			go func() {
				defer close(done)
				for _, client := range []*gateway.Client{a, b, a} {
					notifyDirectory(ready, &lastURL, client)
					if consume {
						select {
						case got := <-ready:
							if got != client {
								t.Error("worker received stale gateway")
							}
						case <-stop:
							return
						}
					}
				}
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				// Release a blocked sender in the old implementation before failing.
				if !consume {
					select {
					case <-ready:
					default:
					}
				}
				t.Fatal("gateway notification blocked")
			}
			if !consume {
				select {
				case got := <-ready:
					if got != a {
						t.Fatal("latest gateway A was lost")
					}
				default:
					t.Fatal("gateway notification missing")
				}
			}
			// Repeated polls of the same gateway should not trigger more pushes.
			notifyDirectory(ready, &lastURL, a)
			select {
			case <-ready:
				t.Fatal("unchanged gateway notified again")
			default:
			}
		})
	}
}

// No sockets: every gateway request goes through this in-process transport.
func TestRunDefaultGatewayPrivacy(t *testing.T) {
	for _, mode := range []string{"empty", "expired", "used", "live", "paired", "explicit"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			wantRequest := mode == "live" || mode == "paired" || mode == "explicit"
			if mode == "explicit" {
				mustRemote(t, SetGateway(root, "https://example.test"))
			} else if mode == "paired" {
				mustRemote(t, update(root, func(c *Config) error {
					c.Devices = []Device{{ID: "peer"}}
					return nil
				}))
			} else if mode != "empty" {
				mustRemote(t, updateInvites(root, func([]invite) ([]invite, error) {
					expires := time.Now().Add(time.Hour).Unix()
					if mode == "expired" {
						expires = time.Now().Add(-time.Second).Unix()
					}
					return []invite{{Expires: expires, Used: mode == "used"}}, nil
				}))
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var requests atomic.Int32
			old := http.DefaultClient
			http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				want := DefaultGateway
				if mode == "explicit" {
					want = "https://example.test"
				}
				if r.URL.Scheme+"://"+r.URL.Host != want {
					t.Errorf("unexpected gateway: %s", r.URL.Host)
				}
				cancel()
				return nil, errors.New("in-process stop")
			})}
			t.Cleanup(func() { http.DefaultClient = old })
			start := time.Now()
			Run(ctx, root, "", nil)
			if got := requests.Load(); (got > 0) != wantRequest {
				t.Fatalf("requests = %d, want request = %v", got, wantRequest)
			}
			if !wantRequest && time.Since(start) < 900*time.Millisecond {
				t.Fatal("idle observation ended before one second")
			}
		})
	}
}
