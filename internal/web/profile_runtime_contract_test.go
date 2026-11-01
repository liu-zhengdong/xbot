package web

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/cliops"
)

func profileRuntimeWeb(t *testing.T) (string, func(string, string, io.Reader, string) (int, []byte)) {
	t.Helper()
	root := t.TempDir()
	mustFolderWebBot(t, root, "old", "u1")
	s := &Server{Root: root, Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(muxOf(s))
	t.Cleanup(host.Close)
	call := func(method, path string, body io.Reader, contentType string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, host.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		res, err := host.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, raw
	}
	return root, call
}

func TestProfileRuntimePatchPreflightsIndependentInputs(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		conflict   bool
	}{
		{"name conflict", `{"name":"taken","description":"changed","persona":"changed"}`, true},
		{"empty persona", `{"name":"next","description":"changed","persona":"   "}`, false},
		{"missing group", `{"name":"next","description":"changed","persona":"changed","group":"missing"}`, false},
		{"invalid combination", `{"name":"next","description":"changed","persona":"changed","timezone":"Bad/Zone"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, call := profileRuntimeWeb(t)
			if tc.conflict {
				mustFolderWebBot(t, root, "taken", "u1")
			}
			config := filepath.Join(bot.Dir(root, "old"), "config.json")
			persona := filepath.Join(bot.Dir(root, "old"), "persona.md")
			before, _ := os.ReadFile(config)
			duty, _ := os.ReadFile(persona)
			status, raw := call(http.MethodPatch, "/api/bots/old", strings.NewReader(tc.body), "application/json")
			if status < 400 {
				t.Fatalf("accepted invalid combination: %d %s", status, raw)
			}
			after, _ := os.ReadFile(config)
			afterDuty, _ := os.ReadFile(persona)
			if !bytes.Equal(before, after) || !bytes.Equal(duty, afterDuty) {
				t.Fatal("preflight failure changed profile/persona")
			}
			if _, err := os.Stat(bot.Dir(root, "next")); !os.IsNotExist(err) {
				t.Fatalf("invalid request moved directory: %v", err)
			}
		})
	}
}

func TestProfileRuntimePartialPersonaAfterMove(t *testing.T) {
	root, call := profileRuntimeWeb(t)
	persona := filepath.Join(bot.Dir(root, "old"), "persona.md")
	if err := os.Remove(persona); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(persona, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(persona, "occupied"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	status, raw := call(http.MethodPatch, "/api/bots/old", strings.NewReader(`{"name":"next","description":"saved before persona I/O","persona":"new duty"}`), "application/json")
	var result cliops.Profile
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("bad receipt: %s %v", raw, err)
	}
	if status < 400 || result.Error == "" || result.Name != "next" || result.PreviousName != "old" || !result.Moved || !result.Applied {
		t.Fatalf("lost actual partial receipt: %d %s", status, raw)
	}
	if _, err := os.Stat(bot.Dir(root, "old")); !os.IsNotExist(err) {
		t.Fatalf("old directory revived: %v", err)
	}
	current, err := bot.Load(root, "next")
	if err != nil {
		t.Fatal(err)
	}
	if current.Config().Description != "saved before persona I/O" {
		t.Fatal("partial save was hidden or undone")
	}
	fields := strings.Join(result.AppliedFields, ",")
	if !strings.Contains(fields, "description") || strings.Contains(fields, "persona") {
		t.Fatalf("inaccurate completed fields: %v", result.AppliedFields)
	}
}

func TestProfileRuntimeAvatarMultipartLimitAndClear(t *testing.T) {
	root, call := profileRuntimeWeb(t)
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	_, _ = rand.New(rand.NewSource(19)).Read(img.Pix)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	imageBytes := encoded.Bytes()
	if len(imageBytes) <= 64<<10 {
		t.Fatal("fixture must exceed profile JSON budget")
	}
	upload := func(data []byte) (int, []byte) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, err := form.CreateFormFile("file", "face.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(data); err != nil {
			t.Fatal(err)
		}
		if err = form.Close(); err != nil {
			t.Fatal(err)
		}
		return call(http.MethodPost, "/api/bots/old/avatar", &body, form.FormDataContentType())
	}
	status, raw := upload(imageBytes)
	if status != http.StatusOK {
		t.Fatalf("real image upload: %d %s", status, raw)
	}
	current, err := bot.Load(root, "old")
	if err != nil {
		t.Fatal(err)
	}
	data, _, ok, err := current.AvatarBytes()
	if err != nil || !ok || !bytes.Equal(data, imageBytes) {
		t.Fatalf("uploaded bytes differ: %v %v", ok, err)
	}
	status, raw = upload(bytes.Repeat([]byte("x"), (1<<20)+1))
	if status < 400 {
		t.Fatalf("accepted file over one MiB: %d %s", status, raw)
	}
	data, _, ok, err = current.AvatarBytes()
	if err != nil || !ok || !bytes.Equal(data, imageBytes) {
		t.Fatal("oversize file changed current image")
	}
	status, raw = call(http.MethodDelete, "/api/bots/old/avatar", nil, "")
	var result cliops.Profile
	if err = json.Unmarshal(raw, &result); err != nil || status != http.StatusOK || result.Avatar != bot.AvatarGenerated || result.AvatarSeed != "old" {
		t.Fatalf("clear receipt: %d %s %v", status, raw, err)
	}
}
