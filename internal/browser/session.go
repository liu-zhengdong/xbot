package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const maxSnap = 8000

// Session is one Chrome page.
type Session struct {
	conn *Conn
	stop func()
}

// Attach dials an existing debugger page.
func Attach(ctx context.Context, wsURL string) (*Session, error) {
	conn, err := Dial(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	return &Session{conn: conn}, nil
}

// Navigate loads an http or https URL.
func (s *Session) Navigate(ctx context.Context, raw string) error {
	if err := checkURL(raw); err != nil {
		return err
	}
	if _, err := s.conn.Call(ctx, "Page.navigate", map[string]string{"url": raw}); err != nil {
		return err
	}
	return nil
}

// Snapshot returns the title, URL, and visible text.
func (s *Session) Snapshot(ctx context.Context) (string, error) {
	raw, err := s.conn.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    "document.title + \"\\n\" + location.href + \"\\n\" + (document.body ? document.body.innerText : \"\")",
		"returnByValue": true,
	})
	if err != nil {
		return "", err
	}
	text, err := evalString(raw)
	if err != nil {
		return "", err
	}
	if len(text) > maxSnap {
		text = text[:maxSnap] + "…"
	}
	return wrapPage(text), nil
}

// wrapPage marks page text as data. A page that contains the closer cannot end the block.
func wrapPage(text string) string {
	text = strings.NewReplacer(
		"</untrusted_event>", "< /untrusted_event>",
		"<untrusted_event>", "< untrusted_event>",
	).Replace(text)
	return "<untrusted_event>\n" + text + "\n</untrusted_event>"
}

// Click clicks the first element matching a CSS selector.
func (s *Session) Click(ctx context.Context, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" || len(selector) > 200 || strings.ContainsAny(selector, "\r\n") {
		return "", errors.New("browser: bad selector")
	}
	expr := fmt.Sprintf("(() => { const el = document.querySelector(%s); if (!el) return \"missing\"; el.click(); return \"clicked\"; })()", strconv.Quote(selector))
	raw, err := s.conn.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": expr, "returnByValue": true,
	})
	if err != nil {
		return "", err
	}
	text, err := evalString(raw)
	if err != nil {
		return "", err
	}
	if text == "missing" {
		return "", errors.New("browser: selector not found")
	}
	return text, nil
}

// Release closes the debugger socket and leaves Chrome running.
func (s *Session) Release() {
	if s == nil {
		return
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.stop = nil
}

// Close disconnects and runs the optional process stopper.
func (s *Session) Close() {
	if s == nil {
		return
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	if s.stop != nil {
		s.stop()
	}
}

func checkURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("browser: url must be http or https")
	}
	return nil
}

func evalString(raw json.RawMessage) (string, error) {
	var env struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", err
	}
	if env.ExceptionDetails.Text != "" {
		return "", fmt.Errorf("browser: %s", env.ExceptionDetails.Text)
	}
	if len(env.Result.Value) == 0 {
		return "", errors.New("browser: empty result")
	}
	var text string
	if err := json.Unmarshal(env.Result.Value, &text); err != nil {
		return "", err
	}
	return text, nil
}
