package account

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ipLimit allows at most max calls per key within a sliding window. It lives
// in memory only, so a restart clears it; that is fine for slowing down
// scripted sign-ups from one address.
type ipLimit struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newIPLimit(max int, window time.Duration) *ipLimit {
	return &ipLimit{max: max, window: window, hits: map[string][]time.Time{}}
}

// allow records a call for key at now, unless key is already at max. An empty
// key (an in-process caller with no HTTP request) is not limited.
func (l *ipLimit) allow(key string, now time.Time) bool {
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, ts := range l.hits {
		keep := ts[:0]
		for _, t := range ts {
			if now.Sub(t) < l.window {
				keep = append(keep, t)
			}
		}
		if len(keep) == 0 {
			delete(l.hits, k)
		} else {
			l.hits[k] = keep
		}
	}
	if len(l.hits[key]) >= l.max {
		return false
	}
	l.hits[key] = append(l.hits[key], now)
	return true
}

type clientIPKey struct{}

// WithClientIP passes the caller's IP, which Register limits by.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

func clientIPFrom(ctx context.Context) string {
	v, _ := ctx.Value(clientIPKey{}).(string)
	return v
}

// clientIP is the address a request came from. The service runs behind
// OpenResty on the same host, which sets X-Real-IP to $remote_addr and so
// overwrites anything the client sent; reached directly, there is no such
// header and RemoteAddr is the peer. X-Forwarded-For is not read: its
// leftmost entry is whatever the client chose to write.
func clientIP(r *http.Request) string {
	if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
		return ip.String()
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
