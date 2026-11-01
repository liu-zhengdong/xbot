package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

// localOwnerClient reuses the local owner's cookie session for subsequent RPCs.
func localOwnerClient(ctx context.Context, addr string) (*http.Client, string, error) {
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	base, err := url.Parse(addr)
	if err != nil || base.Scheme != "http" || base.User != nil || base.Path != "" || base.RawQuery != "" || base.Fragment != "" {
		return nil, "", errors.New("--http must be a local HTTP address")
	}
	host := base.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, "", errors.New("--http must be a loopback address")
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String()+"/api/session", nil)
	if err != nil {
		return nil, "", err
	}
	response, err := client.Do(req)
	if err != nil {
		client.CloseIdleConnections()
		return nil, "", err
	}
	var session struct {
		Authed bool `json:"authed"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&session)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !session.Authed {
		client.CloseIdleConnections()
		return nil, "", errors.New("local owner session unavailable")
	}
	return client, base.String(), nil
}
