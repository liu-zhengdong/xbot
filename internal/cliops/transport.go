package cliops

import (
	"net/http"
	"time"
)

// LocalHTTPClient preserves each caller's timeout while avoiding ambient proxies
// and redirects for explicit local runtime operations.
func LocalHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport:     &http.Transport{Proxy: nil},
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
