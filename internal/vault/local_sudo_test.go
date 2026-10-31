package vault

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestLocalOnlySecretCannotReachHTTP(t *testing.T) {
	s := testStore(t)
	if err := s.Set(LocalSudo, "fixture-local-password", nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.Dir(), s.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if list := reopened.List(); len(list) != 1 || list[0].Value != "" || len(list[0].Domains) != 0 {
		t.Fatal("local metadata contract")
	}
	for _, host := range []string{"localhost", "127.0.0.1", "api.example.com", "sub.example.com"} {
		if HostAllowed(host, nil) {
			t.Fatal("empty domain list allowed injection")
		}
		for _, where := range []string{"url", "header", "body"} {
			req, _ := http.NewRequest("POST", "https://"+host+"/", strings.NewReader("ordinary"))
			switch where {
			case "url":
				req.URL.RawQuery = "key={{secret:LOCAL_SUDO}}"
			case "header":
				req.Header.Set("Authorization", "{{secret:LOCAL_SUDO}}")
			case "body":
				req, _ = http.NewRequest("POST", "https://"+host+"/", strings.NewReader("{{secret:LOCAL_SUDO}}"))
			}
			called := false
			proxy := &Proxy{Store: reopened, Base: roundTripFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, nil })}
			if _, err := proxy.RoundTrip(req); err == nil || called {
				t.Fatalf("%s %s reached egress", host, where)
			}
		}
	}
}

func TestWriteLocalSudoReadsFreshAndOnlyNamedSecret(t *testing.T) {
	s := testStore(t)
	var out bytes.Buffer
	if err := s.Set("OTHER", "fixture-other", []string{"example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteLocalSudo(s.Dir(), s.KeyPath(), &out); err == nil || out.Len() != 0 {
		t.Fatal("missing sudo accepted")
	}
	if err := s.Set(LocalSudo, "fixture-local-password", nil); err != nil {
		t.Fatal(err)
	}
	if err := WriteLocalSudo(s.Dir(), s.KeyPath(), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "fixture-local-password\n" {
		t.Fatal("askpass stdout mismatch")
	}
	out.Reset()
	if err := s.Remove(LocalSudo); err != nil {
		t.Fatal(err)
	}
	if err := WriteLocalSudo(s.Dir(), s.KeyPath(), &out); err == nil || out.Len() != 0 {
		t.Fatal("deleted sudo accepted")
	}
}
