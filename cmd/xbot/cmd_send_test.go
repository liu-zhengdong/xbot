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
)

func TestSendEventHTTP(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			http.SetCookie(w, &http.Cookie{Name: "xbot_session", Value: "test", Path: "/"})
			fmt.Fprint(w, `{"authed":true}`)
		case "/api/bots/ada/events":
			if _, err := r.Cookie("xbot_session"); err != nil {
				t.Error(err)
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, "event: accepted\ndata: {\"id\":\"e1\"}\n\nevent: done\ndata: {\"text\":\"\"}\n\n")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	var out bytes.Buffer
	err := cmdSend([]string{"--http", srv.URL, "--whoami", "codex", "ada", "-"}, strings.NewReader("ping"), &out)
	if err != nil || out.String() != "accepted\nsilent\n" || got["sender"] != "codex" || got["text"] != "ping" {
		t.Fatalf("out %q got %v err %v", out.String(), got, err)
	}
	if err := cmdSend([]string{"ada", "ping"}, strings.NewReader(""), &out); err == nil {
		t.Fatal("missing identity accepted")
	}
}

func TestSendEventNoWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			http.SetCookie(w, &http.Cookie{Name: "xbot_session", Value: "test", Path: "/"})
			fmt.Fprint(w, `{"authed":true}`)
		case "/api/bots/ada/events":
			fmt.Fprint(w, "event: accepted\ndata: {\"id\":\"e1\"}\n\n")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	var out bytes.Buffer
	err := cmdSend([]string{"--http", srv.URL, "--whoami", "codex", "--no-wait", "ada", "ping"}, strings.NewReader(""), &out)
	if err != nil || out.String() != "accepted\n" {
		t.Fatalf("no-wait out %q err %v", out.String(), err)
	}
}

func TestSendProcess(t *testing.T) {
	if os.Getenv("XBOT_SEND_TEST_PROCESS") == "1" {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			http.SetCookie(w, &http.Cookie{Name: "xbot_session", Value: "test", Path: "/"})
			fmt.Fprint(w, `{"authed":true}`)
		case "/api/bots/ada/events":
			var v map[string]string
			if err := json.NewDecoder(r.Body).Decode(&v); err != nil || v["sender"] != "codex" || v["text"] != "ping" {
				t.Errorf("body %v %v", v, err)
			}
			fmt.Fprint(w, "event: accepted\ndata: {\"id\":\"e1\"}\n\nevent: done\ndata: {\"text\":\"done\"}\n\n")
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSendProcess$", "--", "--root", t.TempDir(), "send", "--whoami", "codex", "--http", srv.URL, "ada", "ping")
	cmd.Env = append(os.Environ(), "XBOT_SEND_TEST_PROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "accepted\ndone\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
