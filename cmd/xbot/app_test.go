package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/web"
)

func TestAppLock(t *testing.T) {
	root := t.TempDir()
	lock, err := lockApp(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	second, err := lockApp(root)
	if second != nil {
		second.Release()
		t.Fatal("second lock succeeded")
	}
	if err == nil || err.Error() != "xbot app is already running" {
		t.Fatalf("unexpected lock error: %v", err)
	}
}

func TestRunServeApp(t *testing.T) {
	root := t.TempDir()
	// A file avoids pipe buffering and keeps captured credentials out of failures.
	capture, err := os.Create(filepath.Join(t.TempDir(), "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = capture, capture
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	listening := make(chan string, 1)
	go func() {
		done <- runServe(ctx, root, serveConfig{HTTPAddr: "127.0.0.1:0", Interval: time.Hour, App: true,
			OnListening: func(_ *web.Server, addr net.Addr) { listening <- addr.String() },
		})
	}()
	stopped := false
	defer func() {
		if stopped {
			return
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("serve failed to stop")
		}
	}()
	var addr string
	select {
	case addr = <-listening:
	case err := <-done:
		stopped = true
		cancel()
		t.Fatalf("serve exited before listening: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("listener not ready")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	start := time.Now()
	res, err := client.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 {
		t.Fatal("web UI unavailable")
	}
	t.Logf("local UI GET: %s", time.Since(start))
	cancel()
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve failed to stop")
	}
	raw, err := os.ReadFile(filepath.Join(root, "web-token.json"))
	if err != nil {
		t.Fatal(err)
	}
	var secret struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &secret); err != nil || secret.Token == "" {
		t.Fatal("missing web token")
	}
	output, err := os.ReadFile(capture.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), secret.Token) || strings.Contains(string(output), "login token") {
		t.Error("app leaked login credentials")
	}
}

// A second app on a busy port must fail before it changes the root that the
// running server owns (approval sweep, seed, routine migration).
func TestRunServeAppBusyPortTouchesNothing(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	root := t.TempDir()
	err = runServe(context.Background(), root, serveConfig{HTTPAddr: busy.Addr().String(), Interval: time.Hour, App: true})
	if err == nil {
		t.Fatal("busy port accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "bots")); !os.IsNotExist(err) {
		t.Fatalf("root changed before bind: %v", err)
	}
}

// serve returns only after its routine ticker has finished writing the data
// root, and the ticker does not run again afterwards. Returning earlier raced
// TempDir cleanup in TestRunServeApp (#224).
func TestRunServeWaitsForTimerTicker(t *testing.T) {
	root := t.TempDir()
	var calls, active atomic.Int32
	var returned, late atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	listening := make(chan struct{}, 1)
	go func() {
		done <- runServe(ctx, root, serveConfig{HTTPAddr: "127.0.0.1:0", Interval: time.Hour, App: true,
			OnListening: func(*web.Server, net.Addr) { listening <- struct{}{} },
			// The first call is the synchronous check; later ones are the ticker.
			DeliverTimers: func(string, time.Time) error {
				if returned.Load() {
					late.Store(true)
				}
				active.Add(1)
				defer active.Add(-1)
				if calls.Add(1) == 2 {
					time.Sleep(200 * time.Millisecond)
				}
				return nil
			},
		})
	}()
	select {
	case <-listening:
	case err := <-done:
		t.Fatalf("serve exited before listening: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("listener not ready")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve failed to stop")
	}
	returned.Store(true)
	if active.Load() != 0 {
		t.Fatal("serve returned while the routine ticker was still delivering")
	}
	time.Sleep(300 * time.Millisecond)
	if late.Load() {
		t.Fatal("the routine ticker ran after serve returned")
	}
}
