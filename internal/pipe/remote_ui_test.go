package pipe

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPipeWritesAndContentTypeEndToEnd(t *testing.T) {
	w := newWorld(t)
	w.device.Handler = http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != `{"text":"你好"}` || r.Header.Get("Content-Type") != "application/json" {
			t.Error("request body/content type lost")
		}
		if _, err := r.Cookie("xbot_session"); err != nil {
			t.Error("owner authorization missing")
		}
		out.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = out.Write([]byte(`{"ok":true}`))
	})
	w.startDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tk, err := NewTicket(ctx, w.hubURL, w.entrySes, "mini-1")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Dial(ctx, w.hubURL, tk)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, method := range []string{"POST", "PATCH", "PUT", "DELETE"} {
		code, body, ct, err := c.Do(ctx, method, "/api/test?q=1", []byte(`{"text":"你好"}`), "application/json")
		if err != nil || code != 200 || string(body) != `{"ok":true}` || ct != "application/json; charset=utf-8" {
			t.Fatalf("%s: %d %s %q %v", method, code, body, ct, err)
		}
	}
	for _, path := range []string{"https://example.com/api/test", "//example.com/api/test", "/healthz", "/api/..\\healthz"} {
		code, _, _, err := c.Do(ctx, "POST", path, nil, "")
		if err != nil || code != 403 {
			t.Fatalf("path %q: %d %v", path, code, err)
		}
	}
	if _, _, _, err := c.Do(ctx, "POST", "/api/test", make([]byte, MaxBody+1), ""); err == nil {
		t.Fatal("oversize request accepted")
	}
}

func TestDeviceBoundsAndBufferedSSE(t *testing.T) {
	calls := 0
	d := Device{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: 你好\n\n"))
	})}
	for _, req := range []frame{{Method: "POST", Path: "/api/test", Body: bytes.Repeat([]byte("x"), MaxBody+1)}, {Method: "TRACE", Path: "/api/test"}, {Method: "POST", Path: "/api/test", CT: "x\r\ny"}} {
		res := d.answer(context.Background(), req)
		if res.Status < 400 || calls != 0 {
			t.Fatal("invalid request reached handler")
		}
	}
	res := d.answer(context.Background(), frame{Method: "POST", Path: "/api/test"})
	if res.Status != 200 || res.CT != "text/event-stream" || string(res.Body) != "data: 你好\n\n" {
		t.Fatalf("SSE lost: %+v", res)
	}
	d.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(make([]byte, MaxBody+1)) })
	if res := d.answer(context.Background(), frame{Method: "GET", Path: "/api/test"}); res.Status != 413 {
		t.Fatal("oversize response accepted")
	}
}

func TestTicketCORS(t *testing.T) {
	h := &Hub{Origins: []string{"https://account.babelgo.cn:8443"}}
	for _, method := range []string{"POST", "OPTIONS"} {
		for _, origin := range []string{"", "https://account.babelgo.cn:8443", "https://evil.example"} {
			r := httptest.NewRequest(method, "/pipe/v1/ticket", strings.NewReader(`{}`))
			r.Header.Set("Origin", origin)
			r.Header.Set("Access-Control-Request-Method", "POST")
			r.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
			out := httptest.NewRecorder()
			h.Handler().ServeHTTP(out, r)
			if origin == "https://evil.example" {
				if out.Code != 403 || out.Header().Get("Access-Control-Allow-Origin") != "" {
					t.Fatal("unlisted origin allowed")
				}
			} else {
				if origin != "" && (out.Header().Get("Access-Control-Allow-Origin") != origin || out.Header().Get("Access-Control-Allow-Headers") != "Authorization, Content-Type" || out.Header().Get("Access-Control-Allow-Methods") != "POST, OPTIONS") {
					t.Fatal("CORS headers missing")
				}
				want := 401
				if method == "OPTIONS" {
					want = 204
				}
				if out.Code != want {
					t.Fatalf("%s %q = %d", method, origin, out.Code)
				}
			}
		}
	}
}
