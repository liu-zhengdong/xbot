package remote

import (
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
