//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/browsertest"
)

// This slice keeps the previous independent profile expectations intact. The
// provider and helpers are reused; new assertions observe avatar metadata,
// actual files and real browser pixels through the existing product entries.
func cliAvatarWant(t *testing.T, value map[string]any, avatar, seed string) {
	t.Helper()
	if value["avatar"] != avatar || value["avatar_seed"] != seed {
		t.Fatalf("avatar metadata=%v; want avatar=%q seed=%q", value, avatar, seed)
	}
}

func cliAvatarPNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 12, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 12; x++ {
			img.Set(x, y, color.RGBA{R: 70, G: 90, B: 140, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "uploaded-avatar.png")
	if err := os.WriteFile(path, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func cliAvatarAPI(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	status, raw := srv.do(http.MethodGet, "/api/bots/Worker", nil)
	if status != http.StatusOK {
		t.Fatalf("profile API: HTTP %d", status)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func cliAvatarFiles(t *testing.T, root string) (upload, mime []byte) {
	t.Helper()
	var err error
	upload, err = os.ReadFile(filepath.Join(root, "bots", "Worker", "avatar"))
	if err != nil {
		t.Fatal(err)
	}
	mime, err = os.ReadFile(filepath.Join(root, "bots", "Worker", "avatar.mime"))
	if err != nil {
		t.Fatal(err)
	}
	return upload, mime
}

func cliAvatarNoUpload(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"avatar", "avatar.mime"} {
		if _, err := os.Stat(filepath.Join(root, "bots", "Worker", name)); !os.IsNotExist(err) {
			t.Fatalf("generated seed retained upload file %s: %v", name, err)
		}
	}
}

func TestCLIProfileAvatarChat(t *testing.T) {
	// The first subtest alone is also the narrow old-binary red check: one real
	// chat Shell invokes --avatar-seed, with no all-case failure matrix.
	for _, tc := range []struct{ name, input, seed string }{
		{"seed", "Worker#cli-avatar", "Worker#cli-avatar"},
		{"trimmed_unicode", " 周报#7 ", "周报#7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cliProfileIsolation(t)
			model := newCLIProfileModel(t)
			const cue = "CLI_AVATAR_CHAT"
			model.add(cue, 1,
				`"$XBOT_BIN" profile set --self --avatar-seed `+shellQuote(tc.input)+` --json`,
				`"$XBOT_BIN" profile show --self --json`,
			)
			defer model.release(cue)
			home := t.TempDir()
			root := filepath.Join(home, "data")
			env := append(os.Environ(), "HOME="+home, "XBOT_BASE_URL="+model.server.URL+"/v1", "XBOT_MODEL=smoke", "XBOT_API_KEY=smoke", "XBOT_AUTO_UPGRADE=0")
			cliProfileMustRun(t, env, "--root", root, "new", "Worker")
			launcher := &Server{t: t, Root: root, env: env}
			chat := launcher.Chat("Worker")
			chat.Say(cue)
			cliProfileWaitHeld(t, model, cue)
			value := cliProfileJSON(t, cliProfileResult(t, model, cue, 0))
			cliAvatarWant(t, value, "generated", tc.seed)
			if value["applied"] != true {
				t.Fatalf("seed set did not acknowledge persistence: %v", value)
			}
			cliAvatarWant(t, cliProfileDisk(t, root, "Worker"), "generated", tc.seed)
			model.release(cue)
			WaitFor(t, 10*time.Second, "standalone avatar chat completion", func() (bool, string) {
				return strings.Contains(chat.Stdout(), cue+" completed"), chat.Stdout() + "\n" + chat.Stderr()
			})
			cliAvatarWant(t, cliProfileJSON(t, cliProfileResult(t, model, cue, 1)), "generated", tc.seed)
			chat.Quit()
		})
	}
}

func TestCLIProfileAvatarServeAtomic(t *testing.T) {
	cliProfileIsolation(t)
	model := newCLIProfileModel(t)
	first := Start(t, &Stub{srv: model.server})
	other := Start(t, &Stub{srv: model.server})
	imagePath := cliAvatarPNG(t)
	for _, srv := range []*Server{first, other} {
		srv.CreateBot("Worker", "Independent avatar metadata acceptance.")
		srv.PatchBot("Worker", map[string]any{"description": "before avatar", "timezone": "UTC", "avatar_seed": "Worker#original"})
		// This explicitly offline human command remains supported. No runtime
		// endpoint exists in the command's environment during fixture setup.
		cliProfileMustRun(t, srv.env, "--root", srv.Root, "profile", "avatar", "Worker", "--file", imagePath)
	}
	originalUpload, originalMIME := cliAvatarFiles(t, first.Root)
	otherUpload, otherMIME := cliAvatarFiles(t, other.Root)
	const cue = "CLI_AVATAR_ATOMIC"
	model.add(cue, 6,
		`"$XBOT_BIN" profile show --self --json`,
		`"$XBOT_BIN" profile set --self --avatar-seed '<invalid>' --description rejected-description --json`,
		`"$XBOT_BIN" profile set --self --avatar-seed 'Worker#rejected' --timezone Invalid/Nowhere --description rejected-description --json`,
		`"$XBOT_BIN" profile avatar Worker --clear`,
		`"$XBOT_BIN" profile avatar Worker --file `+shellQuote(imagePath),
		`"$XBOT_BIN" profile show --self --json`,
		`"$XBOT_BIN" profile set --self --avatar-seed 'Worker#runtime' --description 'avatar saved' --timezone Asia/Tokyo --json`,
		`"$XBOT_BIN" profile show --self --json`,
	)
	defer model.release(cue)
	reply := first.BeginSend("Worker", cue)
	cliProfileWaitHeld(t, model, cue)
	for _, index := range []int{0, 5} {
		value := cliProfileJSON(t, cliProfileResult(t, model, cue, index))
		cliProfileWant(t, value, "Worker", "before avatar", "UTC")
		cliAvatarWant(t, value, "custom", "Worker#original")
	}
	for _, index := range []int{1, 2, 3, 4} {
		result := cliProfileResult(t, model, cue, index)
		if strings.HasPrefix(result, "exit 0") || !strings.Contains(result, "exit ") || strings.TrimSpace(commandOutput(result)) == "" {
			t.Fatalf("invalid seed/combination or unsupported runtime avatar command did not fail clearly: %s", result)
		}
	}
	assertUnchanged := func() {
		t.Helper()
		value := cliProfileDisk(t, first.Root, "Worker")
		cliProfileWant(t, value, "Worker", "before avatar", "UTC")
		cliAvatarWant(t, value, "custom", "Worker#original")
		upload, mime := cliAvatarFiles(t, first.Root)
		if !bytes.Equal(upload, originalUpload) || !bytes.Equal(mime, originalMIME) {
			t.Fatal("rejected update changed an existing uploaded avatar or MIME file")
		}
	}
	assertUnchanged()
	cliAvatarWant(t, cliAvatarAPI(t, first), "custom", "Worker#original")
	// The web writer must validate the ordinary-field combination before it
	// discards the same uploaded image; this is a real API request, no Bot calls.
	status, _ := first.do(http.MethodPatch, "/api/bots/Worker", map[string]string{"avatar_seed": "Worker#web-rejected", "timezone": "Invalid/Nowhere", "description": "web-rejected"})
	if status != http.StatusBadRequest {
		t.Fatalf("web profile accepted an invalid seed/timezone combination: HTTP %d", status)
	}
	assertUnchanged()
	model.release(cue)
	reply.Wait()
	for _, index := range []int{6, 7} {
		value := cliProfileJSON(t, cliProfileResult(t, model, cue, index))
		cliProfileWant(t, value, "Worker", "avatar saved", "Asia/Tokyo")
		cliAvatarWant(t, value, "generated", "Worker#runtime")
	}
	cliAvatarWant(t, cliAvatarAPI(t, first), "generated", "Worker#runtime")
	cliAvatarWant(t, cliProfileDisk(t, first.Root, "Worker"), "generated", "Worker#runtime")
	cliAvatarNoUpload(t, first.Root)
	status, _ = first.do(http.MethodGet, "/api/bots/Worker/avatar", nil)
	if status != http.StatusNotFound {
		t.Fatalf("generated avatar still serves the old upload: HTTP %d", status)
	}
	cliAvatarWant(t, cliAvatarAPI(t, other), "custom", "Worker#original")
	cliAvatarWant(t, cliProfileDisk(t, other.Root, "Worker"), "custom", "Worker#original")
	upload, mime := cliAvatarFiles(t, other.Root)
	if !bytes.Equal(upload, otherUpload) || !bytes.Equal(mime, otherMIME) {
		t.Fatal("self avatar update changed another instance's upload")
	}
}

func TestCLIProfileAvatarBrowser(t *testing.T) {
	cliProfileIsolation(t)
	chrome := browsertest.Chrome(t)
	model := newCLIProfileModel(t)
	model.add("CLI_AVATAR_BROWSER_LONG", 2,
		`"$XBOT_BIN" profile set --self --avatar-seed 'Ada#2' --description 'avatar live saved' --timezone Asia/Tokyo --json`,
		`"$XBOT_BIN" profile show --self --json`,
	)
	model.add("CLI_AVATAR_BROWSER_SHORT", -1,
		`"$XBOT_BIN" profile set --self --avatar-seed 'Worker#avatar-short-29' --json`,
	)
	defer model.release("CLI_AVATAR_BROWSER_LONG")
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent avatar CLI and real browser acceptance.")
	srv.PatchBot("Worker", map[string]any{"description": "before avatar", "timezone": "UTC", "avatar_seed": "Worker#original"})
	cliProfileMustRun(t, srv.env, "--root", srv.Root, "profile", "avatar", "Worker", "--file", cliAvatarPNG(t))
	u, _ := url.Parse(srv.URL)
	evidence := os.Getenv("XBOT_CLI_AVATAR_EVIDENCE")
	if evidence == "" {
		evidence = t.TempDir()
	}
	cfg, _ := json.Marshal(map[string]any{"chrome": chrome, "url": srv.URL, "control": model.server.URL, "cookies": srv.client.Jar.Cookies(u), "evidence": evidence})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "testdata/cli-profile-avatar.cjs")
	cmd.Stdin = bytes.NewReader(cfg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real avatar CLI browser: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
	for _, index := range []int{0, 1} {
		cliAvatarWant(t, cliProfileJSON(t, cliProfileResult(t, model, "CLI_AVATAR_BROWSER_LONG", index)), "generated", "Ada#2")
	}
	cliAvatarWant(t, cliProfileJSON(t, cliProfileResult(t, model, "CLI_AVATAR_BROWSER_SHORT", 0)), "generated", "Worker#avatar-short-29")
	cliAvatarWant(t, cliProfileDisk(t, srv.Root, "Worker"), "generated", "Worker#avatar-short-29")
	cliAvatarNoUpload(t, srv.Root)
}
