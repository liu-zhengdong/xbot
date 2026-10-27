package pipe

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbound"
	"xbot/internal/inbox"
)

func postHook(t *testing.T, url string, body []byte, header map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(res.Body)
	return res.StatusCode, out.String()
}

// A sender with only the public URL and the hook secret reaches the device
// through the hub; the device checks the secret, the hub never holds it, and
// the delivery lands in the bot's inbox.
func TestHookIngressThroughHub(t *testing.T) {
	w := newWorld(t)
	b, err := bot.Load(w.root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	signed, err := inbound.AddHook(b, "github", inbound.AuthHMAC, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	token, err := inbound.AddHook(b, "ci", inbound.AuthBearer, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	in := &inbound.Server{Root: w.root}
	w.device.Hooks = in.Handler()
	base := HookBase(w.hubURL, "mini-1")

	// The device is not on standby yet.
	if code, _ := postHook(t, inbound.WebhookURL(base, "ada", "github"), []byte(`{}`), nil); code != http.StatusServiceUnavailable {
		t.Fatalf("offline device: %d", code)
	}
	w.startDevice(t)

	body := []byte(`{"ref":"main","note":"` + secretLine + `"}`)
	if code, _ := postHook(t, inbound.WebhookURL(base, "ada", "github"), body, map[string]string{"X-XBot-Signature": inbound.SignHMAC(signed.Secret, []byte("other"))}); code != http.StatusUnauthorized {
		t.Fatalf("bad signature: %d", code)
	}
	if code, _ := postHook(t, inbound.WebhookURL(base, "ada", "github"), body, map[string]string{"Authorization": "Bearer " + signed.Secret}); code != http.StatusUnauthorized {
		t.Fatalf("token on a signed hook: %d", code)
	}
	if code, out := postHook(t, inbound.WebhookURL(base, "ada", "github"), body, map[string]string{"X-XBot-Signature": inbound.SignHMAC(signed.Secret, body), "X-XBot-Delivery": "d1"}); code != http.StatusAccepted {
		t.Fatalf("signed: %d %s", code, out)
	}
	if code, out := postHook(t, inbound.WebhookURL(base, "ada", "ci"), body, map[string]string{"Authorization": "Bearer " + token.Secret}); code != http.StatusAccepted {
		t.Fatalf("token: %d %s", code, out)
	}
	if code, _ := postHook(t, inbound.WebhookURL(base, "ada", "nope"), body, nil); code != http.StatusNotFound {
		t.Fatalf("unknown hook: %d", code)
	}
	if code, _ := postHook(t, inbound.WebhookURL(base, "ada", "github"), bytes.Repeat([]byte("x"), HookBody+1), nil); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize: %d", code)
	}
	if code, _ := postHook(t, inbound.WebhookURL(HookBase(w.hubURL, "other"), "ada", "github"), body, nil); code != http.StatusServiceUnavailable {
		t.Fatalf("unknown device: %d", code)
	}
	// An escaped slash cannot reach another route on the device.
	if code, _ := postHook(t, base+"/webhook/..%2Fapi/github", body, nil); code == http.StatusAccepted {
		t.Fatalf("escaped path accepted")
	}

	if err := in.Flush(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for ev, ok := box.Head(); ok; ev, ok = box.Head() {
		n++
		if err := box.Ack(ev.ID); err != nil {
			t.Fatal(err)
		}
	}
	if n != 2 {
		t.Fatalf("inbox events %d, want 2", n)
	}
	logs := w.logs.String()
	for _, leak := range []string{signed.Secret, token.Secret, secretLine} {
		if strings.Contains(logs, leak) {
			t.Fatalf("hub log leaks a secret or body:\n%s", logs)
		}
	}
	if !strings.Contains(logs, "answered 202") {
		t.Fatalf("log: %s", logs)
	}
}

func TestHookPath(t *testing.T) {
	for p, ok := range map[string]bool{
		"/webhook/ada/github":      true,
		"/webhook/ada/github/x":    false,
		"/webhook/ada%2F..%2Fx/gh": true, // one segment; inbound rejects the name
		"/api/bots/ada/transcript": false,
		"/webhook/ada/github?x=1":  false,
		"/webhook//github":         false,
		"http://evil/webhook/a/b":  false,
	} {
		if hookPath(p) != ok {
			t.Errorf("hookPath(%q) = %v", p, !ok)
		}
	}
}
