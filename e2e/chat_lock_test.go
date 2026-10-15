package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/proc"
	"xbot/internal/provider"
)

type echoCompactor struct{}

func (echoCompactor) Compact(context.Context, string, []provider.Message) (string, error) {
	return "handoff for the new role", nil
}

func TestIdleChatLetsWebStreamAndFork(t *testing.T) {
	root, client, ui, _ := m5Stack(t)
	ui.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		k, err := m5Kernel(b, client)
		if err != nil {
			return nil, err
		}
		k.Compactor = echoCompactor{}
		return k, nil
	}
	ada := mustM5Bot(t, root, "ada", "You are Ada.")
	k, err := m5Kernel(ada, client)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines := make(chan proc.Line)
	errCh := make(chan error, 1)
	go func() {
		errCh <- proc.Run(ctx, proc.Config{
			K:      k,
			Poll:   20 * time.Millisecond,
			Lines:  lines,
			Accept: func(string) (link.Event, error) { return link.Event{}, nil },
		})
	}()
	time.Sleep(40 * time.Millisecond)

	srv := httptest.NewServer(webMux(ui))
	defer srv.Close()
	webc := loginWeb(t, srv, mustToken(t, ui))

	res := doJSON(t, webc, http.MethodPost, srv.URL+"/api/bots/ada/messages", `{"text":"HELLO_WEB"}`, "")
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "event: done") || !strings.Contains(body, "web hello") {
		t.Fatalf("stream %d %s", res.StatusCode, body)
	}

	res = doJSON(t, webc, http.MethodPost, srv.URL+"/api/bots/ada/fork", `{"name":"reviewer","role":"Review pull requests."}`, "")
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || strings.Contains(body, "bot is busy") {
		t.Fatalf("fork %d %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `"name":"reviewer"`) {
		t.Fatalf("fork body %s", body)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("processor did not stop")
	}
}
