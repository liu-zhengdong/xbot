package vault

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// maxRequestBody caps how much of a request body is scanned and rewritten.
const maxRequestBody = 8 << 20

// placeholder matches {{secret:NAME}} anywhere in a URL, header, or body.
var placeholder = regexp.MustCompile(`\{\{secret:([A-Za-z0-9_.-]+)\}\}`)

// escapedPlaceholder matches the same placeholder after http.NewRequest has
// percent-escaped the braces in a URL path ("%7B%7Bsecret:NAME%7D%7D", any
// case). A token in a Telegram path is only ever seen in this form.
var escapedPlaceholder = regexp.MustCompile(`(?i)%7B%7Bsecret:([A-Za-z0-9_.-]+)%7D%7D`)

// placeholders are both spellings, in the order a value must be substituted.
var placeholders = []*regexp.Regexp{placeholder, escapedPlaceholder}

// Proxy is an http.RoundTripper that injects secrets just before egress.
// Both the plain spelling and the percent-escaped form that net/http writes
// into a URL path resolve to the same value. A request whose placeholders
// cannot be resolved safely is refused.
type Proxy struct {
	Base  http.RoundTripper
	Store *Store
}

// RoundTrip injects placeholders and then delegates. The value is only put on
// the wire, never returned in the response or the error.
func (p *Proxy) RoundTrip(req *http.Request) (*http.Response, error) {
	base := http.RoundTripper(http.DefaultTransport)
	if p != nil && p.Base != nil {
		base = p.Base
	}
	if p == nil || p.Store == nil {
		return base.RoundTrip(req)
	}
	out, err := p.Store.Inject(req)
	if err != nil {
		return nil, err
	}
	resp, err := base.RoundTrip(out)
	if err != nil {
		return nil, p.Store.sanitizeError(err)
	}
	return resp, nil
}

// sanitizeError masks secret values that a transport error may quote back
// (for example inside a rebuilt URL). Clean errors pass through unchanged.
func (s *Store) sanitizeError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if red := s.Redact(msg); red != msg {
		return errors.New(red)
	}
	return err
}

// InjectStream is Inject for a request whose body must not be buffered: it
// substitutes placeholders in the URL and headers only, validates each bound
// secret against the target host, applies the same guard over guardBody (the
// scanned prefix of the streamed body), and clones the request keeping its
// streaming body intact.
func (s *Store) InjectStream(req *http.Request, guardBody []byte) (*http.Request, error) {
	if s == nil || req == nil {
		return req, nil
	}
	if scheme := strings.ToLower(req.URL.Scheme); scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("vault: unsupported URL scheme %q", req.URL.Scheme)
	}
	rawURL := req.URL.String()
	header := req.Header.Clone()
	names := findNames(rawURL, header, nil)
	hosts := []string{normalizeHost(req.URL.Hostname())}
	if req.Host != "" {
		if wire := normalizeHost(req.Host); wire != "" && wire != hosts[0] {
			hosts = append(hosts, wire)
		}
	}
	for _, name := range names {
		sec, ok := s.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("vault: unknown secret %q", name)
		}
		for _, host := range hosts {
			if !HostAllowed(host, sec.Domains) {
				return nil, fmt.Errorf("vault: secret %q is not allowed for %q", name, host)
			}
		}
	}
	rawURL = s.apply(rawURL)
	for key, values := range header {
		for i, value := range values {
			value = s.apply(value)
			if !validHeaderValue(value) {
				return nil, fmt.Errorf("vault: header %q value is invalid after injection", key)
			}
			values[i] = value
		}
		header[key] = values
	}
	if err := s.guard(hosts, rawURL, header, guardBody); err != nil {
		return nil, err
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("vault: cannot rebuild request: " + s.Redact(err.Error()))
	}
	out := req.Clone(req.Context())
	out.URL = u
	out.Header = header
	return out, nil
}

// Inject replaces {{secret:NAME}} in the URL, every header value, and the body.
// It validates every referenced secret and its domain binding before replacing
// anything, so a refused request sends no value at all.
func (s *Store) Inject(req *http.Request) (*http.Request, error) {
	if s == nil || req == nil {
		return req, nil
	}
	if scheme := strings.ToLower(req.URL.Scheme); scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("vault: unsupported URL scheme %q", req.URL.Scheme)
	}
	body, err := drainBody(req)
	if err != nil {
		return nil, err
	}
	rawURL := req.URL.String()
	header := req.Header.Clone()
	names := findNames(rawURL, header, body)
	// The dial host is the URL host; the wire Host header can differ and both
	// must be bound before a value is placed on the request.
	hosts := []string{normalizeHost(req.URL.Hostname())}
	if req.Host != "" {
		if wire := normalizeHost(req.Host); wire != "" && wire != hosts[0] {
			hosts = append(hosts, wire)
		}
	}
	for _, name := range names {
		sec, ok := s.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("vault: unknown secret %q", name)
		}
		for _, host := range hosts {
			if !HostAllowed(host, sec.Domains) {
				return nil, fmt.Errorf("vault: secret %q is not allowed for %q", name, host)
			}
		}
	}
	rawURL = s.apply(rawURL)
	for key, values := range header {
		for i, value := range values {
			value = s.apply(value)
			if !validHeaderValue(value) {
				return nil, fmt.Errorf("vault: header %q value is invalid after injection", key)
			}
			values[i] = value
		}
		header[key] = values
	}
	body = []byte(s.apply(string(body)))
	if err := s.guard(hosts, rawURL, header, body); err != nil {
		return nil, err
	}
	return cloneRequest(s, req, rawURL, header, body)
}

// guard refuses a request that carries a secret value — literal or in a common
// encoding — to a host not bound to it. This keeps a copied value (for example
// a redirect that repeats a header or echoes it into the redirect URL) from
// reaching another domain even though no placeholder is left.
func (s *Store) guard(hosts []string, rawURL string, header http.Header, body []byte) error {
	s.mu.Lock()
	type bound struct {
		name    string
		value   string
		domains []string
	}
	list := make([]bound, 0, len(s.secrets))
	for _, sec := range s.secrets {
		if sec.Value != "" {
			list = append(list, bound{name: sec.Name, value: sec.Value, domains: sec.Domains})
		}
	}
	s.mu.Unlock()
	if len(list) == 0 {
		return nil
	}
	text := rawURL
	for _, values := range header {
		for _, value := range values {
			text += "\n" + value
		}
	}
	text += "\n" + string(body)
	lower := ""
	for _, sec := range list {
		hit := false
		for _, form := range redactForms(sec.value) {
			if strings.Contains(text, form) {
				hit = true
				break
			}
			if strings.Contains(form, "%") {
				// Percent-encoding hex case is the attacker's choice.
				if lower == "" {
					lower = strings.ToLower(text)
				}
				if strings.Contains(lower, strings.ToLower(form)) {
					hit = true
					break
				}
			}
		}
		if !hit {
			continue
		}
		for _, host := range hosts {
			if !HostAllowed(host, sec.domains) {
				return fmt.Errorf("vault: secret %q is not allowed for %q", sec.name, host)
			}
		}
	}
	return nil
}

// validHeaderValue mirrors the transport's check: no control bytes except tab.
// Rejecting here keeps an injected value that cannot travel on the wire from
// producing a late, less predictable failure.
func validHeaderValue(v string) bool {
	for i := 0; i < len(v); i++ {
		if b := v[i]; (b < ' ' && b != '\t') || b == 0x7f {
			return false
		}
	}
	return true
}

func (s *Store) apply(text string) string {
	if !strings.Contains(text, "secret:") {
		return text
	}
	for _, re := range placeholders {
		text = re.ReplaceAllStringFunc(text, func(match string) string {
			sub := re.FindStringSubmatch(match)
			if len(sub) != 2 {
				return match
			}
			if sec, ok := s.Lookup(sub[1]); ok {
				return sec.Value
			}
			return match
		})
	}
	return text
}

func findNames(rawURL string, header http.Header, body []byte) []string {
	found := map[string]bool{}
	scan := func(text string) {
		for _, re := range placeholders {
			for _, sub := range re.FindAllStringSubmatch(text, -1) {
				found[sub[1]] = true
			}
		}
	}
	scan(rawURL)
	keys := make([]string, 0, len(header))
	for key := range header {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range header[key] {
			scan(value)
		}
	}
	scan(string(body))
	out := make([]string, 0, len(found))
	for name := range found {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// HostAllowed reports whether host may receive a value bound to domains.
// A domain allows itself and its subdomains; the dot boundary keeps
// evil-a.com and a.com.evil.com out of a binding for a.com.
func HostAllowed(host string, domains []string) bool {
	host = normalizeHost(host)
	if host == "" {
		return false
	}
	for _, d := range domains {
		d = normalizeDomain(d)
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// normalizeHost lowercases a request host, strips brackets, port, and the
// trailing dot. Only ASCII hosts are accepted: some non-ASCII letters fold
// onto ASCII ones when lowercased (the Kelvin sign becomes "k"), which would
// let a Unicode lookalike match a binding while dialing a different name.
// Store IDN bindings in their punycode form.
func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || !isASCII(host) {
		return ""
	}
	host = strings.ToLower(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return strings.TrimSuffix(host, ".")
}

func drainBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	defer req.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(req.Body, maxRequestBody+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxRequestBody {
		return nil, errors.New("vault: request body too large")
	}
	return raw, nil
}

// cloneRequest rebuilds the request around the injected URL, headers, and
// body. A rebuild failure is redacted before it is returned: the parse error
// quotes the whole URL, which by now carries secret values.
func cloneRequest(s *Store, req *http.Request, rawURL string, header http.Header, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	out, err := http.NewRequestWithContext(req.Context(), req.Method, rawURL, reader)
	if err != nil {
		return nil, errors.New("vault: cannot rebuild request: " + s.Redact(err.Error()))
	}
	out.Header = header
	out.Host = req.Host
	out.Close = req.Close
	out.TransferEncoding = append([]string(nil), req.TransferEncoding...)
	out.Trailer = req.Trailer
	return out, nil
}
