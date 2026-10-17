package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	maxRedirects  = 5
	maxFetchBytes = 1 << 20
	fetchTimeout  = 20 * time.Second
	webUserAgent  = "xbot-web/1"
)

var (
	errBlocked   = errors.New("refused: address is not public")
	errRebind    = errors.New("refused: DNS changed to a non-public address")
	errRedirects = errors.New("too many redirects")
)

// Guard fetches one public http(s) URL. It resolves DNS before dialing, refuses
// a second lookup that is no longer public, and checks again on every redirect.
// ProxyFromEnvironment is not used: a proxy would skip the address check.
type Guard struct {
	Timeout      time.Duration
	MaxRedirects int
	MaxBytes     int64
	// Lookup resolves a hostname. Nil uses the system resolver. Tests supply one.
	Lookup func(ctx context.Context, host string) ([]net.IP, error)
	// Dial connects to an already-checked ip:port. Nil uses net.Dialer.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
	// Trusted is an explicit allow-list for DNS answers that are not public
	// (for example 198.18.0.0/15 on a box whose resolver rewrites every name).
	// Literal IPs in the request URL or in a redirect are never matched
	// against Trusted.
	Trusted []netip.Prefix
}

// FetchResult is a bounded response body. Truncated is set when the body hit MaxBytes.
type FetchResult struct {
	URL         string
	Status      int
	StatusLine  string
	ContentType string
	Body        []byte
	Truncated   bool
}

// Fetch performs one public GET. Redirects are followed only while each hop
// stays public, up to MaxRedirects.
func (g *Guard) Fetch(ctx context.Context, raw string) (FetchResult, error) {
	return g.do(ctx, http.MethodGet, raw, nil, nil, true)
}

// Post sends one public POST and does not follow redirects. A redirect would
// move a credential header onto another host, so the caller sees the status
// and stops.
func (g *Guard) Post(ctx context.Context, raw string, body []byte, header http.Header) (FetchResult, error) {
	return g.do(ctx, http.MethodPost, raw, body, header, false)
}

func (g *Guard) do(ctx context.Context, method, raw string, body []byte, header http.Header, follow bool) (FetchResult, error) {
	if g == nil {
		g = &Guard{}
	}
	if err := ctx.Err(); err != nil {
		return FetchResult{}, err
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = fetchTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	hops := g.MaxRedirects
	if hops <= 0 {
		hops = maxRedirects
	}
	if !follow {
		hops = 0
	}
	current := strings.TrimSpace(raw)
	client := g.client()
	for hop := 0; hop <= hops; hop++ {
		if err := validateURL(current); err != nil {
			return FetchResult{}, err
		}
		var rdr io.Reader
		if body != nil && method != http.MethodGet {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, current, rdr)
		if err != nil {
			return FetchResult{}, err
		}
		req.Header.Set("User-Agent", webUserAgent)
		if method == http.MethodGet {
			req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.1")
		}
		for k, vals := range header {
			for _, v := range vals {
				req.Header.Add(k, v)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return FetchResult{}, redactNet(err)
		}
		if follow && isRedirect(resp.StatusCode) {
			loc := resp.Header.Get("Location")
			resp.Body.Close()
			if hop == hops {
				return FetchResult{}, errRedirects
			}
			next, err := redirectURL(current, loc)
			if err != nil {
				return FetchResult{}, err
			}
			current = next
			method = http.MethodGet
			body = nil
			continue
		}
		limit := g.maxBytes()
		rawBody, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		resp.Body.Close()
		if err != nil {
			return FetchResult{}, err
		}
		truncated := int64(len(rawBody)) > limit
		if truncated {
			rawBody = rawBody[:limit]
		}
		final := current
		if resp.Request != nil && resp.Request.URL != nil {
			final = resp.Request.URL.String()
		}
		return FetchResult{
			URL:         final,
			Status:      resp.StatusCode,
			StatusLine:  resp.Status,
			ContentType: resp.Header.Get("Content-Type"),
			Body:        rawBody,
			Truncated:   truncated,
		}, nil
	}
	return FetchResult{}, errRedirects
}

func (g *Guard) maxBytes() int64 {
	if g.MaxBytes > 0 {
		return g.MaxBytes
	}
	return maxFetchBytes
}

func (g *Guard) client() *http.Client {
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           g.dialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          2,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (g *Guard) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errBlocked
	}
	ips, err := g.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	target := net.JoinHostPort(ips[0].String(), port)
	if g.Dial != nil {
		return g.Dial(ctx, "tcp", target)
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	return d.DialContext(ctx, "tcp", target)
}

// resolve returns public addresses for host. A name is looked up twice; if the
// second answer is not public, the call is a DNS rebind and is refused.
func (g *Guard) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip, ok := literalIP(host); ok {
		if !PublicIP(ip) {
			return nil, errBlocked
		}
		return []net.IP{ip}, nil
	}
	if blockedName(host) {
		return nil, errBlocked
	}
	first, err := g.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if err := g.vetResolved(first); err != nil {
		return nil, err
	}
	second, err := g.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if err := g.vetResolved(second); err != nil {
		return nil, errRebind
	}
	return second, nil
}

func (g *Guard) lookup(ctx context.Context, host string) ([]net.IP, error) {
	if g.Lookup != nil {
		return g.Lookup(ctx, host)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP)
	}
	return out, nil
}

func (g *Guard) vetResolved(ips []net.IP) error {
	if len(ips) == 0 {
		return errBlocked
	}
	for _, ip := range ips {
		if PublicIP(ip) || g.trusted(ip) {
			continue
		}
		return errBlocked
	}
	return nil
}

func (g *Guard) trusted(ip net.IP) bool {
	if g == nil || len(g.Trusted) == 0 {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	for _, p := range g.Trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return errors.New("url must be absolute http or https")
	}
	if u.User != nil {
		return errors.New("url must not contain credentials")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return errors.New("url must be absolute http or https")
	}
	if len(raw) > 8192 {
		return errors.New("url is too long")
	}
	return nil
}

func redirectURL(base, loc string) (string, error) {
	ref, err := url.Parse(strings.TrimSpace(loc))
	if err != nil || loc == "" {
		return "", errors.New("redirect missing location")
	}
	bu, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	next := bu.ResolveReference(ref).String()
	if err := validateURL(next); err != nil {
		return "", err
	}
	return next, nil
}

func isRedirect(code int) bool {
	return code == http.StatusMovedPermanently || code == http.StatusFound ||
		code == http.StatusSeeOther || code == http.StatusTemporaryRedirect ||
		code == http.StatusPermanentRedirect
}

func redactNet(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if errors.Is(err, errBlocked) || errors.Is(err, errRebind) || strings.Contains(msg, errBlocked.Error()) || strings.Contains(msg, errRebind.Error()) {
		return err
	}
	return fmt.Errorf("fetch failed")
}
