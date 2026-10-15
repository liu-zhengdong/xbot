package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestCheckURLAndLoopback(t *testing.T) {
	if err := checkURL("file:///etc/passwd"); err == nil {
		t.Fatal("file url accepted")
	}
	if err := checkURL("https://user:pass@example.com"); err == nil {
		t.Fatal("userinfo accepted")
	}
	if err := checkURL("https://example.com/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(context.Background(), "ws://example.com/devtools"); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatal(err)
	}
}

func TestCDPSnapshotAndClick(t *testing.T) {
	ws, closeSrv := fakeCDP(t)
	defer closeSrv()
	ctx := context.Background()
	sess, err := Attach(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Release()
	if err := sess.Navigate(ctx, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	text, err := sess.Snapshot(ctx)
	if err != nil || !strings.Contains(text, "Example") || !strings.Contains(text, "https://example.com") {
		t.Fatalf("%q %v", text, err)
	}
	got, err := sess.Click(ctx, "#go")
	if err != nil || got != "clicked" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := sess.Click(ctx, "bad\nselector"); err == nil {
		t.Fatal("newline selector accepted")
	}
}

func TestConfirmOff(t *testing.T) {
	root := t.TempDir()
	if err := writeMode(root, "off"); err != nil {
		t.Fatal(err)
	}
	l := Open(root)
	if _, _, err := (openTool{l}).Confirm([]byte(`{"url":"https://example.com"}`)); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatal(err)
	}
	if _, _, err := (snapTool{l}).Confirm(nil); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatal(err)
	}
}

func TestConfirmAsk(t *testing.T) {
	root := t.TempDir()
	l := Open(root)
	need, _, err := (snapTool{l}).Confirm(nil)
	if err != nil || need {
		t.Fatalf("allow snapshot need=%v %v", need, err)
	}
	need, summary, err := (openTool{l}).Confirm([]byte(`{"url":"https://example.com"}`))
	if err != nil || !need || !strings.Contains(summary, "example.com") {
		t.Fatalf("need=%v summary=%q %v", need, summary, err)
	}
	if err := writeMode(root, "ask"); err != nil {
		t.Fatal(err)
	}
	need, _, err = (snapTool{l}).Confirm(nil)
	if err != nil || !need {
		t.Fatalf("ask snapshot need=%v %v", need, err)
	}
}

func writeMode(root, mode string) error {
	return os.WriteFile(root+"/machines.json", []byte(`{"machines":{"local":"`+mode+`"}}`+"\n"), 0600)
}

func fakeCDP(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", 500)
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		if err := rw.Flush(); err != nil {
			return
		}
		for {
			opcode, payload, err := readFrame(rw.Reader)
			if err != nil {
				return
			}
			if opcode != 1 {
				continue
			}
			var req struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(payload, &req) != nil {
				continue
			}
			var result any
			switch req.Method {
			case "Page.navigate":
				result = map[string]any{}
			case "Runtime.evaluate":
				text := "Example\nhttps://example.com\nhello"
				if strings.Contains(string(payload), "querySelector") {
					text = "clicked"
				}
				result = map[string]any{"result": map[string]any{"type": "string", "value": text}}
			default:
				result = map[string]any{}
			}
			raw, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
			if err := writeServer(rw, raw); err != nil {
				return
			}
		}
	})}
	go srv.Serve(ln)
	return "ws://" + ln.Addr().String() + "/devtools", func() {
		_ = srv.Close()
		_ = ln.Close()
	}
}

func writeServer(w *bufio.ReadWriter, payload []byte) error {
	if len(payload) > 125 {
		return io.ErrShortWrite
	}
	if _, err := w.Write([]byte{0x81, byte(len(payload))}); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return w.Flush()
}
