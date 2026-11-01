//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Add a CRC-valid ancillary tEXt chunk before IEND: the boundary fixture is
// genuinely a PNG, rather than a small image followed by arbitrary garbage.
func cliFullAvatarPNG(t *testing.T, size int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	base := encoded.Bytes()
	data := make([]byte, size-len(base)-12)
	copy(data, "Comment\x00")
	const padding = "CLI_FULL_AVATAR_PADDING_"
	for i := 8; i < len(data); i++ {
		data[i] = padding[(i-8)%len(padding)]
	}
	var out bytes.Buffer
	out.Write(base[:len(base)-12])
	_ = binary.Write(&out, binary.BigEndian, uint32(len(data)))
	out.WriteString("tEXt")
	out.Write(data)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(append([]byte("tEXt"), data...)))
	out.Write(base[len(base)-12:])
	if _, err := png.Decode(bytes.NewReader(out.Bytes())); err != nil {
		t.Fatalf("boundary PNG is invalid: %v", err)
	}
	return out.Bytes()
}

// Bounded real requests keep diagnostics away from serve stderr/login tokens.
func cliFullAvatarHTTP(t *testing.T, srv *Server, method, route string, body []byte, limit int64) (int, []byte, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, srv.URL+route, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.client.Do(req)
	if err != nil {
		t.Fatalf("avatar fixture %s %s: %v", method, route, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(raw)) > limit {
		t.Fatalf("avatar fixture %s %s: response bytes=%d, read error=%v", method, route, len(raw), err)
	}
	return resp.StatusCode, raw, resp.Header.Get("Content-Type")
}

func cliFullAvatarTurn(t *testing.T, srv *Server, model *cliProfileModel, cue string, commands ...string) ([]string, int) {
	t.Helper()
	model.add(cue, -1, commands...)
	defer func() {
		// The old fixture scans all historical user cues. Retire the finished
		// case so the next natural turn cannot choose an earlier phase.
		model.mu.Lock()
		delete(model.cases, cue)
		model.mu.Unlock()
	}()
	body, _ := json.Marshal(map[string]string{"text": cue})
	status, raw, _ := cliFullAvatarHTTP(t, srv, http.MethodPost, "/api/bots/Worker/messages", body, 64<<10)
	if status != http.StatusOK || !bytes.Contains(raw, []byte("event: done")) || bytes.Contains(raw, []byte("event: error")) {
		t.Fatalf("avatar phase %s did not complete: HTTP %d, SSE bytes=%d", cue, status, len(raw))
	}
	notices := 0
	for _, block := range bytes.Split(raw, []byte("\n\n")) {
		if bytes.HasPrefix(block, []byte("event: profile\n")) {
			notices++
			var notice struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(block, []byte("event: profile\ndata:"))), &notice) != nil || notice.Name != "Worker" {
				t.Fatal("avatar profile SSE did not identify the actual Bot")
			}
		}
	}
	results := make([]string, len(commands))
	for i := range commands {
		results[i] = model.results(cue)[fmt.Sprintf("%s_%d", cue, i)]
		if results[i] == "" || len(results[i]) > 64<<10 {
			t.Fatalf("avatar phase %s command %d: missing or oversized Shell result", cue, i)
		}
	}
	for _, value := range append(results, string(raw)) {
		if strings.Contains(value, "CLI_FULL_AVATAR_PADDING_") || strings.Contains(value, "iVBORw0KGgo") || strings.Contains(value, "\x89PNG") {
			t.Fatal("image bytes/base64 leaked into Shell stdout or messages/profile SSE")
		}
	}
	return results, notices
}

func cliFullAvatarJSON(t *testing.T, result, avatar string, fields ...string) {
	t.Helper()
	var receipt struct {
		Name, Avatar, Scope string
		Applied             bool
		AppliedFields       []string `json:"applied_fields"`
	}
	if !strings.HasPrefix(result, "exit 0\n") || json.Unmarshal([]byte(commandOutput(result)), &receipt) != nil {
		t.Fatalf("avatar CLI did not return successful bounded JSON (result bytes=%d)", len(result))
	}
	if receipt.Name != "Worker" || receipt.Avatar != avatar || receipt.Scope != "runtime" {
		t.Fatalf("avatar CLI metadata: name=%q avatar=%q scope=%q", receipt.Name, receipt.Avatar, receipt.Scope)
	}
	for _, field := range fields {
		if !receipt.Applied || !slices.Contains(receipt.AppliedFields, field) {
			t.Fatalf("avatar CLI did not report actual completed stage %q: applied=%v fields=%v", field, receipt.Applied, receipt.AppliedFields)
		}
	}
}

func TestCLIProfileFullRuntimeAvatar(t *testing.T) {
	cliProfileIsolation(t)
	model := newCLIProfileModel(t)
	srv := Start(t, &Stub{srv: model.server})
	status, _, _ := cliFullAvatarHTTP(t, srv, http.MethodPost, "/api/bots", []byte(`{"name":"Worker","persona":"Local avatar acceptance. BOTNAME=Worker"}`), 64<<10)
	if status != http.StatusOK {
		t.Fatalf("create avatar fixture Bot: HTTP %d", status)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return shellQuote(path)
	}
	imageBytes := cliFullAvatarPNG(t, 1<<20)
	valid := write("boundary.png", imageBytes)
	oversize := write("oversize.png", cliFullAvatarPNG(t, (1<<20)+1))
	invalid := write("invalid.png", []byte("local ASCII file, not an image"))
	jsonBody := []byte(`{"description":"JSON budget must reject before saving"}`)
	jsonFile := write("oversize-profile.json", append(jsonBody, bytes.Repeat([]byte(" "), (64<<10)+1-len(jsonBody))...))
	upload, notices := cliFullAvatarTurn(t, srv, model, "CLI_FULL_AVATAR_UPLOAD",
		`"$XBOT_BIN" profile avatar --self --file `+valid+` --json`,
		`"$XBOT_BIN" profile show --self --json`)
	cliFullAvatarJSON(t, upload[0], "custom", "avatar_file", "avatar_mime", "avatar")
	cliFullAvatarJSON(t, upload[1], "custom")
	if notices == 0 {
		t.Fatal("successful upload did not publish a profile SSE notice")
	}
	status, served, mime := cliFullAvatarHTTP(t, srv, http.MethodGet, "/api/bots/Worker/avatar", nil, 1<<20)
	if status != http.StatusOK || mime != "image/png" || !bytes.Equal(served, imageBytes) {
		t.Fatalf("exact 1MiB upload was not served unchanged: HTTP %d MIME=%q bytes=%d", status, mime, len(served))
	}
	botDir := filepath.Join(srv.Root, "bots", "Worker")
	snapshot := map[string][]byte{}
	for _, name := range []string{"avatar", "avatar.mime", "config.json"} {
		var err error
		snapshot[name], err = os.ReadFile(filepath.Join(botDir, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(snapshot["avatar"], imageBytes) || strings.TrimSpace(string(snapshot["avatar.mime"])) != "image/png" {
		t.Fatal("upload bytes/MIME on disk disagree with the actual upload")
	}
	rejected, notices := cliFullAvatarTurn(t, srv, model, "CLI_FULL_AVATAR_REJECT",
		`"$XBOT_BIN" profile avatar --self --file `+oversize+` --json`,
		`"$XBOT_BIN" profile avatar --self --file `+invalid+` --json`,
		`curl -sS --max-time 5 -o /dev/null -w '%{http_code}' -X PATCH -H 'Content-Type: application/json' -H "X-XBot-Root: $XBOT_ROOT" --data-binary @`+jsonFile+` "$XBOT_ENDPOINT/v1/profile"`)
	for _, result := range rejected[:2] {
		if !strings.HasPrefix(result, "exit ") || strings.HasPrefix(result, "exit 0\n") || strings.TrimSpace(commandOutput(result)) == "" {
			t.Fatal("oversize/type-invalid CLI upload did not fail clearly")
		}
	}
	code := strings.TrimSpace(commandOutput(rejected[2]))
	if !strings.HasPrefix(rejected[2], "exit 0\n") || code != "400" && code != "413" || notices != 0 {
		t.Fatalf("invalid uploads/64KiB+1 source JSON were not rejected without profile notices: HTTP=%q notices=%d", code, notices)
	}
	for name, before := range snapshot {
		after, err := os.ReadFile(filepath.Join(botDir, name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("rejected request changed %s: read error=%v", name, err)
		}
	}
	cleared, notices := cliFullAvatarTurn(t, srv, model, "CLI_FULL_AVATAR_CLEAR",
		`"$XBOT_BIN" profile avatar --self --clear --json`,
		`"$XBOT_BIN" profile show --self --json`)
	cliFullAvatarJSON(t, cleared[0], "generated", "avatar")
	cliFullAvatarJSON(t, cleared[1], "generated")
	if notices == 0 {
		t.Fatal("successful clear did not publish a profile SSE notice")
	}
	for _, name := range []string{"avatar", "avatar.mime"} {
		if _, err := os.Stat(filepath.Join(botDir, name)); !os.IsNotExist(err) {
			t.Fatalf("clear retained uploaded %s: %v", name, err)
		}
	}
	status, _, _ = cliFullAvatarHTTP(t, srv, http.MethodGet, "/api/bots/Worker/avatar", nil, 64<<10)
	if status != http.StatusNotFound {
		t.Fatalf("clear still serves the old upload: HTTP %d", status)
	}
}
