package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/work"
)

// TestPastedImageNeedsNoText is the regression for "图片粘不进去": the composer
// used to require text (the server refused an empty message), there was no
// upload endpoint at all, and the transcript had nowhere to record a picture.
// This goes through the same three steps the browser does: upload, send with
// the id and no text, then read the line back.
func TestPastedImageNeedsNoText(t *testing.T) {
	root, ui, cookie, shots := pasteTestServer(t, true)
	defer ui.Close()

	id := uploadPaste(t, ui.URL, cookie, pngBytes(t, 8, 8))
	if !work.ValidPastedID(id) {
		t.Fatalf("upload returned %q, not a stored id", id)
	}
	// The bytes are on disk in the bot workspace, where view_image looks.
	path, err := work.PastedPath(work.BotDir(root, "ada"), id)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err != nil || st.Size() == 0 {
		t.Fatalf("stored file: %v", err)
	}

	// No text at all: the picture is the message.
	body := fmt.Sprintf(`{"images":[%q]}`, id)
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("message with only a picture: %d %s", res.StatusCode, res.Body)
	}

	// The transcript names the picture, so a reloaded page still shows it.
	var page struct {
		Messages []apiRow `json:"messages"`
	}
	if err := json.Unmarshal(getAuth(t, ui.URL+"/api/bots/ada/transcript", cookie), &page); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range page.Messages {
		if row.Role == provider.RoleUser && len(row.Images) == 1 && row.Images[0] == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("transcript does not name the picture: %+v", page.Messages)
	}

	// The turn that answered it was sent the bytes.
	bodies := shots()
	if len(bodies) == 0 {
		t.Fatal("no model request was made")
	}
	if !strings.Contains(bodies[0], `"type":"image_url"`) {
		t.Fatalf("model request carries no picture: %s", bodies[0])
	}
	want := base64.StdEncoding.EncodeToString(pngBytes(t, 8, 8))
	if !strings.Contains(bodies[0], want[:32]) {
		t.Fatal("the model request does not carry the uploaded bytes")
	}

	// A later turn sees the note, not the bytes: the request stays small.
	res = postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"再说一句"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("second message: %d %s", res.StatusCode, res.Body)
	}
	bodies = shots()
	last := bodies[len(bodies)-1]
	if strings.Contains(last, `"type":"image_url"`) {
		t.Fatalf("a later turn resent the picture: %s", last)
	}
	if !strings.Contains(last, "paste/"+id) {
		t.Fatalf("a later turn does not say which picture was sent: %s", last)
	}
}

// TestPasteRejectsWhatTheModelCannotRead keeps a broken upload out of the
// transcript: the server checks the type and the pixel cap before storing.
func TestPasteRejectsWhatTheModelCannotRead(t *testing.T) {
	_, ui, cookie, _ := pasteTestServer(t, true)
	defer ui.Close()

	cases := []struct {
		name  string
		bytes []byte
		want  int
	}{
		{"text is not a picture", []byte("just words"), http.StatusBadRequest},
		{"empty", nil, http.StatusBadRequest},
		{"over 10 MB", append(pngBytes(t, 8, 8), bytes.Repeat([]byte{0}, (10<<20)+1)...), http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		req, err := http.NewRequest(http.MethodPost, ui.URL+"/api/bots/ada/paste", bytes.NewReader(c.bytes))
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != c.want {
			t.Errorf("%s: %d, want %d", c.name, res.StatusCode, c.want)
		}
	}

	// A name the server never made is refused, so a message cannot point at
	// some other file in the workspace.
	for _, id := range []string{"../config.json", "x.png", strings.Repeat("a", 32) + ".txt"} {
		body := fmt.Sprintf(`{"images":[%q]}`, id)
		res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, body)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("message naming %q: %d, want 400", id, res.StatusCode)
		}
	}

	// The fifth picture on one message is refused before the turn starts.
	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		ids = append(ids, uploadPaste(t, ui.URL, cookie, pngBytes(t, 4, 4)))
	}
	raw, _ := json.Marshal(ids)
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"images":`+string(raw)+`}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("five pictures in one message: %d, want 400", res.StatusCode)
	}
}

// pasteTestServer builds one bot served the way `xbot serve` does it, with a
// model that answers every request and records the body.
func pasteTestServer(t *testing.T, vision bool) (string, *httptest.Server, *http.Cookie, func() []string) {
	t.Helper()
	root := t.TempDir()
	model, shots := recordingModel(t)
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	dir, _, err := work.Open(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	client := &provider.Client{BaseURL: model.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: model.Client()}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{
			Bot: b, Provider: client, Memory: memory.Open(root), MaxSteps: 2,
			Budget: compact.DefaultBudget(), WorkDir: dir,
			Links: []link.Link{link.Web{}, link.NewToolsLinkAt(dir, "")},
			// The bound model is what decides whether a picture is sent.
			Machine:  func() (string, error) { return "allow", nil },
			Selector: provider.FixedSelector{P: client},
		}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	return root, ui, loginCookie(t, ui.URL, tok), shots
}

// recordingModel is a minimal OpenAI-compatible endpoint: it records every
// request body and answers with one text message, so a test can inspect what
// the model was actually sent. It accepts image content parts, which is what
// the shared mock cannot do (it decodes content as a string).
func recordingModel(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"看到了\\n\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

func uploadPaste(t *testing.T, base string, cookie *http.Cookie, data []byte) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/api/bots/ada/paste", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "image/png")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("upload %d %s", res.StatusCode, raw)
	}
	var out struct {
		Image string `json:"image"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Image == "" {
		t.Fatalf("upload returned no id: %s", raw)
	}
	return out.Image
}

// pngBytes makes a real PNG, so the server's byte sniffing accepts it.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// storedPastePath is where the workspace keeps a pasted picture; used by tests
// that need to look at the file itself.
func storedPastePath(root, name, id string) string {
	return filepath.Join(work.BotDir(root, name), work.PastedDir, id)
}

// TestPasteRefusesLinksAndOversize is the regression for three holes the first
// cut had: a symlink in the paste directory was followed (so any file on the
// machine could be served or shown to the model), an oversized picture was
// accepted because the downscale was thrown away, and pictures that fit one by
// one but not together were accepted and then silently trimmed.
func TestPasteRefusesLinksAndOversize(t *testing.T) {
	root, ui, cookie, _ := pasteTestServer(t, true)
	defer ui.Close()

	// A picture over the edge cap is refused instead of stored.
	wide := pngBytes(t, 2100, 8)
	req, _ := http.NewRequest(http.MethodPost, ui.URL+"/api/bots/ada/paste", bytes.NewReader(wide))
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !bytes.Contains(raw, []byte("2100")) {
		t.Fatalf("oversize picture: %d %s", res.StatusCode, raw)
	}

	// A picture grown past the cap after upload is refused on read, so a bot
	// cannot make the process load an arbitrarily large file by rewriting one.
	grown := uploadPaste(t, ui.URL, cookie, pngBytes(t, 4, 4))
	grownPath, err := work.PastedPath(work.BotDir(root, "ada"), grown)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grownPath, append(pngBytes(t, 4, 4), bytes.Repeat([]byte{0}, (10<<20)+16)...), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _ := getStatus(t, ui.URL+"/api/bots/ada/paste/"+grown, cookie); code != http.StatusNotFound {
		t.Errorf("GET on a picture grown past the cap: %d, want 404", code)
	}

	// A symlink with a valid name is not a stored picture.
	dir := work.PastedDirFor(work.BotDir(root, "ada"))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(secret, pngBytes(t, 4, 4), 0600); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("ab", 16) + ".png"
	if err := os.Symlink(secret, filepath.Join(dir, id)); err != nil {
		t.Fatal(err)
	}
	if code, _ := getStatus(t, ui.URL+"/api/bots/ada/paste/"+id, cookie); code != http.StatusNotFound {
		t.Errorf("GET on a symlinked name: %d, want 404", code)
	}
	res2 := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, fmt.Sprintf(`{"images":[%q]}`, id))
	if res2.StatusCode != http.StatusBadRequest {
		t.Errorf("message naming a symlink: %d, want 400", res2.StatusCode)
	}

	// Two pictures that fit alone but not together are refused at the message.
	big := pngBytesNoise(t, 1400, 1400)
	if len(big) <= 5<<20 {
		t.Fatalf("test picture is only %d bytes; it needs to exceed half the total cap", len(big))
	}
	a := uploadPaste(t, ui.URL, cookie, big)
	b := uploadPaste(t, ui.URL, cookie, big)
	body := fmt.Sprintf(`{"images":[%q,%q]}`, a, b)
	res3 := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, body)
	if res3.StatusCode != http.StatusBadRequest || !bytes.Contains(res3.Body, []byte("加起来")) {
		t.Fatalf("two pictures over the total cap: %d %s", res3.StatusCode, res3.Body)
	}
}

func getStatus(t *testing.T, url string, cookie *http.Cookie) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

// pngBytesNoise makes a PNG that compresses poorly, so a test can build a
// picture too big to send beside another one.
func pngBytesNoise(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*1664525 + 1013904223
			img.Set(x, y, color.RGBA{R: uint8(seed >> 24), G: uint8(seed >> 16), B: uint8(seed >> 8), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestPasteDirThatIsALinkIsRefused covers the case a per-component check
// misses: the paste directory itself is a link, so a picture in the directory
// it points at would be served as this bot's own. Reading goes through the
// workspace root, so it is refused, and writing refuses it too.
func TestPasteDirThatIsALinkIsRefused(t *testing.T) {
	root, ui, cookie, _ := pasteTestServer(t, true)
	defer ui.Close()

	dir := work.PastedDirFor(work.BotDir(root, "ada"))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := os.Symlink(other, dir); err != nil {
		t.Fatal(err)
	}
	// A picture in the directory the link points at.
	otherPic := filepath.Join(other, strings.Repeat("cd", 16)+".png")
	if err := os.WriteFile(otherPic, pngBytes(t, 4, 4), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _ := getStatus(t, ui.URL+"/api/bots/ada/paste/"+filepath.Base(otherPic), cookie); code != http.StatusNotFound {
		t.Errorf("GET through a linked paste directory: %d, want 404", code)
	}
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie,
		fmt.Sprintf(`{"images":[%q]}`, filepath.Base(otherPic)))
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("message naming a picture behind a linked paste directory: %d, want 400", res.StatusCode)
	}

	// Uploading must not write through the link either.
	before, err := os.ReadDir(other)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, ui.URL+"/api/bots/ada/paste", bytes.NewReader(pngBytes(t, 4, 4)))
	req.AddCookie(cookie)
	up, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(up.Body)
	up.Body.Close()
	after, err := os.ReadDir(other)
	if err != nil {
		t.Fatal(err)
	}
	if up.StatusCode == http.StatusOK || len(after) != len(before) {
		t.Fatalf("upload wrote through a linked paste directory: %d %s", up.StatusCode, raw)
	}
}
