package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestMessageProcess(t *testing.T) {
	if os.Getenv("XBOT_MESSAGE_TEST_PROCESS") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				if err := run(os.Args[i+1:]); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				os.Exit(0)
			}
		}
		os.Exit(2)
	}
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			http.SetCookie(w, &http.Cookie{Name: "xbot_session", Value: "test", Path: "/"})
			fmt.Fprint(w, `{"authed":true}`)
		case "/api/bots/机器人/messages":
			if r.Method != "POST" || r.Header.Get("Origin") != "" {
				t.Errorf("unexpected request: %s origin %q", r.Method, r.Header.Get("Origin"))
			}
			if _, err := r.Cookie("xbot_session"); err != nil {
				t.Error(err)
			}
			var body struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			got = body.Text
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: delta\ndata: {\"text\":\"状态\"}\n\nevent: done\ndata: {\"text\":\"最终回答\"}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMessageProcess$", "--", "--root", t.TempDir(), "message", "机器人", "hello", "world", "--http", srv.URL)
	cmd.Env = append(os.Environ(), "XBOT_MESSAGE_TEST_PROCESS=1")
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != "最终回答\n" || got != "hello world" {
		t.Fatalf("output=%q err=%v message=%q", output, err, got)
	}
}

func TestMessageFailuresAndInput(t *testing.T) {
	tests := []struct {
		name, response string
		status         int
		delay          time.Duration
		want           string
	}{
		{"error", "event: error\ndata: {\"error\":\"boom\"}\n\n", 200, 0, "boom"},
		{"missing", "missing", 404, 0, "404"},
		{"handoff", "event: done\ndata: {\"handed_off\":true}\n\n", 200, 0, "handed off"},
		{"truncated", "event: delta\ndata: {}\n\n", 200, 0, "without done"},
		{"timeout", "", 200, 100 * time.Millisecond, "deadline exceeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/session" {
					http.SetCookie(w, &http.Cookie{Name: "xbot_session", Value: "test", Path: "/"})
					fmt.Fprint(w, `{"authed":true}`)
					return
				}
				time.Sleep(tt.delay)
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.response)
			}))
			defer srv.Close()
			var out bytes.Buffer
			err := cmdMessage([]string{"--http", srv.URL, "--timeout", "30ms", "bot", "-"}, strings.NewReader("stdin text"), &out)
			if err == nil || !strings.Contains(err.Error(), tt.want) || out.Len() != 0 {
				t.Fatalf("err=%v out=%q", err, out.String())
			}
		})
	}
}

func TestMessageTextSources(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		input string
		want  string
	}{
		{[]string{"bot", "--text", "explicit text"}, "ignored", "explicit text"},
		{[]string{"bot"}, "stdin text", "stdin text"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/session" {
				http.SetCookie(w, &http.Cookie{Name: "xbot_session", Value: "test", Path: "/"})
				fmt.Fprint(w, `{"authed":true}`)
				return
			}
			var body struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Text != tc.want {
				t.Errorf("text=%q err=%v", body.Text, err)
			}
			fmt.Fprint(w, "event: done\ndata: {\"text\":\"ok\"}\n\n")
		}))
		var out bytes.Buffer
		err := cmdMessage(append([]string{"--http", srv.URL}, tc.args...), strings.NewReader(tc.input), &out)
		srv.Close()
		if err != nil || out.String() != "ok\n" {
			t.Fatalf("out=%q err=%v", out.String(), err)
		}
	}
}
