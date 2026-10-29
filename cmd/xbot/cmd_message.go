package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// cmdMessage is an HTTP client of the already-running owner server.
func cmdMessage(args []string, in io.Reader, out io.Writer) error {
	addr, timeout, explicit := "127.0.0.1:8788", 2*time.Minute, ""
	var positional []string
	textSet := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--help" || a == "-h" {
			_, err := fmt.Fprintln(out, "Usage: xbot [--root DIR] message [--http ADDR] [--timeout DURATION] <bot> <text...|-> [--text TEXT]\nDefault: --http 127.0.0.1:8788, --timeout 2m. With no text or with -, read stdin.")
			return err
		}
		if a == "--http" || a == "--timeout" || a == "--text" {
			if i+1 == len(args) {
				return fmt.Errorf("%s needs a value", a)
			}
			i++
			switch a {
			case "--http":
				addr = args[i]
			case "--timeout":
				var err error
				timeout, err = time.ParseDuration(args[i])
				if err != nil || timeout <= 0 {
					return errors.New("invalid --timeout")
				}
			case "--text":
				explicit, textSet = args[i], true
			}
		} else {
			positional = append(positional, a)
		}
	}
	if len(positional) < 1 || (textSet && len(positional) != 1) {
		return errors.New("usage: xbot message [--http ADDR] [--timeout DURATION] <bot> <text...|-> [--text TEXT]")
	}
	var message string
	switch {
	case textSet:
		message = explicit
	case len(positional) == 1 || (len(positional) == 2 && positional[1] == "-"):
		b, err := io.ReadAll(io.LimitReader(in, 16<<10+1))
		if err != nil {
			return err
		}
		message = string(b)
	default:
		message = strings.Join(positional[1:], " ")
	}
	if strings.TrimSpace(message) == "" || len(message) > 16<<10 {
		return errors.New("message must be nonempty and at most 16 KiB")
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	base, err := url.Parse(addr)
	if err != nil || base.Scheme != "http" || base.User != nil || base.Path != "" || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("--http must be a local HTTP address")
	}
	host := base.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("--http must be a loopback address")
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	sessionReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String()+"/api/session", nil)
	if err != nil {
		return err
	}
	res, err := client.Do(sessionReq)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	var session struct {
		Authed bool `json:"authed"`
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&session)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK || !session.Authed {
		return errors.New("local owner session unavailable")
	}
	payload, _ := json.Marshal(map[string]string{"text": message})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String()+"/api/bots/"+url.PathEscape(positional[0])+"/messages", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("message HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var event string
	var data []string
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if event == "done" || event == "error" {
				var result struct {
					Text       string `json:"text"`
					Error      string `json:"error"`
					HandedOff  bool   `json:"handed_off"`
					Background string `json:"background"`
				}
				if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &result); err != nil {
					return fmt.Errorf("invalid %s event: %w", event, err)
				}
				if event == "error" {
					return fmt.Errorf("message failed: %s", result.Error)
				}
				if result.HandedOff || result.Background != "" {
					return errors.New("message response handed off or running in background; no reply received")
				}
				if result.Text == "" {
					return errors.New("message completed without visible reply")
				}
				_, err := fmt.Fprintln(out, result.Text)
				return err
			}
			event, data = "", nil
		} else if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("message stream: %w", err)
	}
	return errors.New("message stream ended without done or error")
}
