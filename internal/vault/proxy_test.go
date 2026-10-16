package vault

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProxyInjectsURLHeadersBody(t *testing.T) {
	const value = "tok-abc-123"
	var gotQuery, gotHeader, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotHeader = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	s := testStore(t)
	if err := s.Set("TOKEN", value, []string{srvHost(t, srv.URL)}); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &Proxy{Store: s}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v?t={{secret:TOKEN}}", strings.NewReader("body={{secret:TOKEN}}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer {{secret:TOKEN}}")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if gotQuery != "t="+value {
		t.Fatalf("query %q", gotQuery)
	}
	if gotHeader != "Bearer "+value {
		t.Fatalf("header %q", gotHeader)
	}
	if gotBody != "body="+value {
		t.Fatalf("body %q", gotBody)
	}
}

func TestProxyRefusesUnboundAndLookalikeDomains(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	s := testStore(t)
	if err := s.Set("TOKEN", "value", []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"http://evil-a.com/x",
		"http://a.com.evil.com/x",
		"http://sub.a.com.evil.com/x",
		"http://a.com:8080@evil-a.com/x",
	}
	for _, raw := range cases {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer {{secret:TOKEN}}")
		if _, err := s.Inject(req); err == nil {
			t.Fatalf("%s was not refused", raw)
		}
	}
	// A request to the real server with the host not bound must not dial.
	client := &http.Client{Transport: &Proxy{Store: s}}
	if _, err := client.Get(srv.URL + "/x?t={{secret:TOKEN}}"); err == nil {
		t.Fatal("unbound request was sent")
	}
	if hits != 0 {
		t.Fatalf("server was hit %d times", hits)
	}
}

func TestHostAllowedBoundaries(t *testing.T) {
	cases := []struct {
		host    string
		domains []string
		want    bool
	}{
		{"a.com", []string{"a.com"}, true},
		{"api.a.com", []string{"a.com"}, true},
		{"a.com", []string{"b.com"}, false},
		{"evil-a.com", []string{"a.com"}, false},
		{"a.com.evil.com", []string{"a.com"}, false},
		{"A.COM", []string{"a.com"}, true},
		{"a.com", []string{"A.COM"}, true},
		{"example.com", []string{"a.com", "example.com"}, true},
	}
	for _, c := range cases {
		if got := HostAllowed(c.host, c.domains); got != c.want {
			t.Fatalf("HostAllowed(%q, %v) = %v, want %v", c.host, c.domains, got, c.want)
		}
	}
}

func TestProxyRefusesLiteralValueToUnboundHost(t *testing.T) {
	s := testStore(t)
	if err := s.Set("TOKEN", "literal-value-9", []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://evil-a.com/x", nil)
	req.Header.Set("X-Token", "literal-value-9")
	if _, err := s.Inject(req); err == nil {
		t.Fatal("literal value sent to an unbound host")
	}
	allowed, _ := http.NewRequest(http.MethodGet, "http://a.com/x", nil)
	allowed.Header.Set("X-Token", "literal-value-9")
	if _, err := s.Inject(allowed); err != nil {
		t.Fatalf("bound host refused: %v", err)
	}
}

func TestProxyUnknownSecretAndNoPlaceholder(t *testing.T) {
	s := testStore(t)
	req, _ := http.NewRequest(http.MethodGet, "http://a.com/x", nil)
	req.Header.Set("Authorization", "Bearer {{secret:MISSING}}")
	if _, err := s.Inject(req); err == nil || !strings.Contains(err.Error(), "unknown secret") {
		t.Fatalf("err %v", err)
	}

	body := "plain body"
	req, _ = http.NewRequest(http.MethodPost, "http://a.com/x", strings.NewReader(body))
	out, err := s.Inject(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != body {
		t.Fatalf("body changed: %q", raw)
	}
}

func TestProxyRefusesNonHTTPScheme(t *testing.T) {
	s := testStore(t)
	if err := s.Set("TOKEN", "value", []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "ftp://a.com/x?t={{secret:TOKEN}}", nil)
	if _, err := s.Inject(req); err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("err %v", err)
	}
}

func TestProxyRefusesNonASCIIHostsAndDomains(t *testing.T) {
	s := testStore(t)
	// The Kelvin sign lowercases onto "k", so this host folds toward "ak.com".
	if err := s.Set("TOKEN", "value", []string{"ak.com"}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://a\u212A.com/x", nil)
	req.Header.Set("Authorization", "Bearer {{secret:TOKEN}}")
	if _, err := s.Inject(req); err == nil {
		t.Fatal("Unicode lookalike host accepted")
	}
	if HostAllowed("a\u212A.com", []string{"ak.com"}) {
		t.Fatal("HostAllowed folded a Unicode host onto an ASCII binding")
	}
	// Bindings themselves must be ASCII; punycode is the ASCII form of an IDN.
	if err := s.Set("IDN", "value", []string{"a\u212A.com"}); err == nil {
		t.Fatal("Unicode domain binding accepted")
	}
	if err := s.Set("IDN", "value", []string{"xn--fiqs8s.example"}); err != nil {
		t.Fatalf("punycode binding rejected: %v", err)
	}
}

func TestProxyHostOverrideMustBeBound(t *testing.T) {
	s := testStore(t)
	if err := s.Set("TOKEN", "value", []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://a.com/x", nil)
	req.Host = "evil.com"
	req.Header.Set("Authorization", "Bearer {{secret:TOKEN}}")
	if _, err := s.Inject(req); err == nil {
		t.Fatal("unbound wire Host accepted")
	}
	req, _ = http.NewRequest(http.MethodGet, "http://a.com/x", nil)
	req.Host = "api.a.com"
	req.Header.Set("Authorization", "Bearer {{secret:TOKEN}}")
	if _, err := s.Inject(req); err != nil {
		t.Fatalf("bound wire Host refused: %v", err)
	}
}

func TestProxyRefusesHeaderOrURLItCannotCarry(t *testing.T) {
	const value = "line1\nline2"
	s := testStore(t)
	if err := s.Set("TOKEN", value, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://a.com/x", nil)
	req.Header.Set("X-Token", "{{secret:TOKEN}}")
	_, err := s.Inject(req)
	if err == nil || strings.Contains(err.Error(), value) {
		t.Fatalf("control character in header value: err %v", err)
	}
	req, _ = http.NewRequest(http.MethodGet, "http://a.com/x?t={{secret:TOKEN}}", nil)
	_, err = s.Inject(req)
	if err == nil {
		t.Fatal("control character in URL accepted")
	}
	if strings.Contains(err.Error(), value) {
		t.Fatalf("rebuild error leaks the value: %v", err)
	}
}

func TestProxySanitizesTransportErrors(t *testing.T) {
	const value = "err-secret-value"
	s := testStore(t)
	if err := s.Set("TOKEN", value, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	failing := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf(`Get "http://a.com/?t=` + value + `": boom`)
	})
	client := &http.Client{Transport: &Proxy{Store: s, Base: failing}}
	_, err := client.Get("http://a.com/x?t={{secret:TOKEN}}")
	if err == nil {
		t.Fatal("transport error swallowed")
	}
	if strings.Contains(err.Error(), value) {
		t.Fatalf("error leaks the value: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A redirect that carries the value to an unbound host — literally or encoded —
// must be refused before the second hop is dialed.
func TestProxyRefusesRedirectCarryingValue(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/start" {
			return
		}
		// Redirect to "localhost": a different hostname to the guard even
		// though it resolves to this same server.
		port := r.Host[strings.Index(r.Host, ":"):]
		http.Redirect(w, r, "http://localhost"+port+"/hop?"+r.URL.RawQuery, http.StatusFound)
	}))
	defer srv.Close()

	s := testStore(t)
	if err := s.Set("TOKEN", "tok/red=1", []string{srvHost(t, srv.URL)}); err != nil {
		t.Fatal(err)
	}
	carrying := map[string]string{
		"literal":    "c=tok/red=1",
		"url-escape": "c=" + url.QueryEscape("tok/red=1"),
		"base64":     "c=" + base64.StdEncoding.EncodeToString([]byte("tok/red=1")),
	}
	client := &http.Client{Transport: &Proxy{Store: s}}
	for name, carry := range carrying {
		before := hits
		_, err := client.Get(srv.URL + "/start?" + carry)
		if err == nil {
			t.Fatalf("%s redirect leak was followed", name)
		}
		if hits != before+1 {
			t.Fatalf("%s redirect reached the second hop", name)
		}
	}
}

func TestGuardRefusesEncodedValueToUnboundHost(t *testing.T) {
	const value = "tok/red=1"
	s := testStore(t)
	if err := s.Set("TOKEN", value, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	carrying := map[string]string{
		"literal":    value,
		"url-escape": url.QueryEscape(value),
		"lower-hex":  strings.ToLower(url.QueryEscape(value)),
		"base64":     base64.StdEncoding.EncodeToString([]byte(value)),
	}
	for name, carry := range carrying {
		req, _ := http.NewRequest(http.MethodGet, "http://evil.com/x?t="+carry, nil)
		if _, err := s.Inject(req); err == nil {
			t.Fatalf("%s form of the value reached an unbound host", name)
		}
	}
	// The bound host still receives it.
	req, _ := http.NewRequest(http.MethodGet, "http://a.com/x?t="+url.QueryEscape(value), nil)
	if _, err := s.Inject(req); err != nil {
		t.Fatalf("bound host refused: %v", err)
	}
}

func srvHost(t *testing.T, raw string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req.URL.Hostname()
}
