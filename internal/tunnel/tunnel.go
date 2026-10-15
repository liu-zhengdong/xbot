// Package tunnel publishes an already-running xbot HTTP listener.
// Cloudflare and Tailscale are transports. The kernel does not see them.
package tunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	// Cloudflare runs cloudflared quick tunnel against the local listener.
	Cloudflare = "cloudflare"
	// Tailscale uses the machine's tailnet IPv4. A loopback --http is rebound to it.
	Tailscale = "tailscale"
)

// Resolved is the listen address and, for Tailscale, the URL clients should use.
// Cloudflare fills PublicURL only after StartCloudflare.
type Resolved struct {
	Mode       string
	ListenAddr string
	PublicURL  string
}

// Resolve checks the mode and, for Tailscale, the tailnet address.
// An empty mode returns the listen address unchanged.
func Resolve(ctx context.Context, mode, listenAddr, tailscaleCmd string) (Resolved, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		return Resolved{}, errors.New("tunnel: empty listen address")
	}
	if _, _, err := net.SplitHostPort(listenAddr); err != nil {
		return Resolved{}, fmt.Errorf("tunnel: listen address: %w", err)
	}
	switch mode {
	case "":
		return Resolved{ListenAddr: listenAddr}, nil
	case Cloudflare:
		return Resolved{Mode: Cloudflare, ListenAddr: listenAddr}, nil
	case Tailscale:
		ip, err := TailscaleIP(ctx, tailscaleCmd)
		if err != nil {
			return Resolved{}, err
		}
		bind, public := TailscaleListen(listenAddr, ip)
		return Resolved{Mode: Tailscale, ListenAddr: bind, PublicURL: public}, nil
	default:
		return Resolved{}, fmt.Errorf("tunnel: unknown mode %q (cloudflare or tailscale)", mode)
	}
}

// TailscaleIP runs `tailscale ip -4` and returns the first address.
func TailscaleIP(ctx context.Context, command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		command = "tailscale"
	}
	cmd := exec.CommandContext(ctx, command, "ip", "-4")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("tunnel: tailscale ip -4: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("tunnel: tailscale ip -4: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if ip := net.ParseIP(line); ip != nil && ip.To4() != nil {
			return ip.String(), nil
		}
	}
	return "", errors.New("tunnel: tailscale ip -4 returned no IPv4 address")
}

// TailscaleListen rewrites a loopback bind onto the tailnet address.
// Any other bind is left as the owner set it. The public URL always uses the tailnet address and the listen port.
func TailscaleListen(listenAddr, ip string) (bind, public string) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return listenAddr, ""
	}
	if loopback(host) {
		bind = net.JoinHostPort(ip, port)
	} else {
		bind = listenAddr
	}
	return bind, "http://" + net.JoinHostPort(ip, port)
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// LocalURL is the address cloudflared should dial. Wildcard binds become 127.0.0.1.
func LocalURL(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String()
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// Proc is a cloudflared quick tunnel. Close stops it.
type Proc struct {
	PublicURL string
	cmd       *exec.Cmd
	cancel    context.CancelFunc
}

// Close stops cloudflared.
func (p *Proc) Close() {
	if p == nil {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Wait()
	}
}

// StartCloudflare runs cloudflared against localURL and returns once the public URL is printed.
func StartCloudflare(ctx context.Context, localURL, command string, wait time.Duration) (*Proc, error) {
	localURL = strings.TrimSpace(localURL)
	if localURL == "" {
		return nil, errors.New("tunnel: empty local url")
	}
	if strings.TrimSpace(command) == "" {
		command = "cloudflared"
	}
	if wait <= 0 {
		wait = 45 * time.Second
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, command, "tunnel", "--no-autoupdate", "--url", localURL)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("tunnel: cloudflared: %w", err)
	}
	url, err := waitURL(io.MultiReader(stdout, stderr), wait)
	if err != nil {
		cancel()
		_ = cmd.Wait()
		return nil, err
	}
	return &Proc{PublicURL: url, cmd: cmd, cancel: cancel}, nil
}

var urlPattern = regexp.MustCompile(`https://[A-Za-z0-9][A-Za-z0-9.-]*`)

// ParsePublicURL returns the quick-tunnel URL from cloudflared output.
func ParsePublicURL(text string) (string, bool) {
	var fallback string
	for _, match := range urlPattern.FindAllString(text, -1) {
		host := strings.TrimPrefix(match, "https://")
		if strings.EqualFold(host, "trycloudflare.com") || strings.HasSuffix(strings.ToLower(host), ".trycloudflare.com") {
			return match, true
		}
		if fallback == "" && keepURL(host) {
			fallback = match
		}
	}
	if fallback == "" {
		return "", false
	}
	return fallback, true
}

func keepURL(host string) bool {
	h := strings.ToLower(host)
	switch h {
	case "localhost", "127.0.0.1", "cloudflare.com", "www.cloudflare.com", "api.cloudflare.com":
		return false
	}
	if strings.HasSuffix(h, ".cloudflare.com") && !strings.HasSuffix(h, ".trycloudflare.com") {
		return false
	}
	return true
}

func waitURL(r io.Reader, wait time.Duration) (string, error) {
	found := make(chan string, 1)
	done := make(chan struct{})
	var buf strings.Builder
	go func() {
		defer close(done)
		sc := bufio.NewReader(r)
		tmp := make([]byte, 1024)
		sent := false
		for {
			n, err := sc.Read(tmp)
			if n > 0 && !sent {
				buf.Write(tmp[:n])
				if url, ok := ParsePublicURL(buf.String()); ok {
					found <- url
					sent = true
					buf.Reset()
				} else if buf.Len() > 64<<10 {
					s := buf.String()
					buf.Reset()
					buf.WriteString(s[len(s)-32<<10:])
				}
			}
			if err != nil {
				return
			}
		}
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case url := <-found:
		return url, nil
	case <-done:
		if url, ok := ParsePublicURL(buf.String()); ok {
			return url, nil
		}
		tail := buf.String()
		if len(tail) > 500 {
			tail = tail[len(tail)-500:]
		}
		tail = strings.TrimSpace(tail)
		if tail == "" {
			return "", errors.New("tunnel: cloudflared exited before printing a public url")
		}
		return "", fmt.Errorf("tunnel: cloudflared exited before printing a public url: %s", tail)
	case <-timer.C:
		return "", errors.New("tunnel: timed out waiting for the cloudflared public url")
	}
}
