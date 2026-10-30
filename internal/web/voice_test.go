package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/work"
)

// A real mono PCM recording, so both the upload seam and browser can consume it.
func voiceWAV() []byte {
	data := make([]byte, 44+1600)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 1600)
	return data
}

func voiceRequest(t *testing.T, method, url string, cookie *http.Cookie, data []byte, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, res.Header, body
}

func uploadVoice(t *testing.T, base string, cookie *http.Cookie, data []byte) string {
	t.Helper()
	code, _, raw := voiceRequest(t, "POST", base+"/api/bots/ada/voice", cookie, data, map[string]string{"Content-Type": "audio/wav"})
	if code != 200 {
		t.Fatalf("upload voice: HTTP %d %s", code, raw)
	}
	var out struct {
		Audio string `json:"audio"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Audio == "" {
		t.Fatalf("upload returned no audio: %s", raw)
	}
	return out.Audio
}

// Runs upload -> message -> real kernel -> transcript -> authenticated ranged GET.
// The fake chat endpoint records what the Agent actually sees; no speech endpoint
// is called behind its back and the original owner text remains untouched.
func TestVoiceHTTPJourney(t *testing.T) {
	root, ui, cookie, shots := pasteTestServer(t, false)
	defer ui.Close()
	data := voiceWAV()
	id := uploadVoice(t, ui.URL, cookie, data)
	stored, err := os.ReadFile(filepath.Join(work.BotDir(root, "ada"), "voice", id))
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatalf("voice bytes on disk: %v", err)
	}
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, fmt.Sprintf(`{"audios":[%q]}`, id))
	if res.StatusCode != 200 {
		t.Fatalf("audio-only message: %d %s", res.StatusCode, res.Body)
	}
	var page struct {
		Messages []apiRow `json:"messages"`
	}
	if err := json.Unmarshal(getAuth(t, ui.URL+"/api/bots/ada/transcript", cookie), &page); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range page.Messages {
		if row.Role == "user" && len(row.Audios) == 1 && row.Audios[0] == id {
			found = true
			if row.Content != "" {
				t.Fatalf("audio replaced owner text: %q", row.Content)
			}
		}
	}
	if !found {
		t.Fatalf("audio attachment missing from transcript: %+v", page.Messages)
	}
	bodies := shots()
	if len(bodies) == 0 || !strings.Contains(bodies[0], "voice/"+id) || !strings.Contains(bodies[0], "listen") {
		t.Fatalf("Agent did not receive playable file path and listening route: %v", bodies)
	}
	if strings.Contains(bodies[0], `input_audio`) || strings.Contains(bodies[0], `audio_url`) {
		t.Fatal("audio was injected directly into chat instead of letting Agent listen")
	}
	res = postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"再看上一条语音"}`)
	if res.StatusCode != 200 {
		t.Fatalf("later turn: %d", res.StatusCode)
	}
	bodies = shots()
	if !strings.Contains(bodies[len(bodies)-1], "voice/"+id) {
		t.Fatal("later context lost the audio path")
	}
	for _, format := range []string{"md", "jsonl"} {
		raw := getAuth(t, ui.URL+"/api/bots/ada/export?format="+format, cookie)
		if !strings.Contains(string(raw), id) || (format == "md" && !strings.Contains(string(raw), "audio: voice/"+id)) || (format == "jsonl" && !strings.Contains(string(raw), `"audios"`)) {
			t.Fatalf("%s export lost audio attachment: %s", format, raw)
		}
	}
	code, h, raw := voiceRequest(t, "GET", ui.URL+"/api/bots/ada/voice/"+id, cookie, nil, map[string]string{"Range": "bytes=0-43"})
	if code != 206 || len(raw) != 44 || !bytes.Equal(raw, data[:44]) || h.Get("Content-Type") != "audio/wav" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("ranged playback: HTTP %d len=%d headers=%v", code, len(raw), h)
	}
	code, _, _ = voiceRequest(t, "GET", ui.URL+"/api/bots/ada/voice/"+id, nil, nil, nil)
	if code != 401 {
		t.Fatalf("anonymous audio GET: HTTP %d", code)
	}
}

func TestVoiceHTTPRejectsInvalidAttachments(t *testing.T) {
	root, ui, cookie, _ := pasteTestServer(t, false)
	defer ui.Close()
	for _, tc := range []struct {
		name   string
		data   []byte
		status int
	}{{"empty", nil, 400}, {"text", []byte("not audio"), 400}, {"oversized", make([]byte, 10<<20+1), 413}} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := voiceRequest(t, "POST", ui.URL+"/api/bots/ada/voice", cookie, tc.data, map[string]string{"Content-Type": "audio/wav"})
			if code != tc.status {
				t.Fatalf("HTTP %d, want %d", code, tc.status)
			}
		})
	}
	id := uploadVoice(t, ui.URL, cookie, voiceWAV())
	for _, body := range []string{`{"audios":["../config.json"]}`, `{"audios":["` + strings.Repeat("a", 32) + `.wav"]}`, fmt.Sprintf(`{"audios":[%q,%q,%q,%q,%q]}`, id, id, id, id, id)} {
		res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, body)
		if res.StatusCode != 400 {
			t.Fatalf("bad audio message accepted: HTTP %d body=%s", res.StatusCode, body)
		}
	}
	// A syntactically valid id from another Bot is not a cross-Bot attachment.
	other, err := bot.Create(root, "private", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err = other.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "POST"} {
		url := ui.URL + "/api/bots/private/voice"
		if method == "GET" {
			url += "/" + id
		}
		code, _, _ := voiceRequest(t, method, url, cookie, voiceWAV(), nil)
		if code != 404 {
			t.Fatalf("other owner's %s voice: HTTP %d", method, code)
		}
	}
	// Even a symlink bearing a valid id cannot escape this Bot's workspace.
	outside := filepath.Join(t.TempDir(), "outside.wav")
	if err = os.WriteFile(outside, voiceWAV(), 0600); err != nil {
		t.Fatal(err)
	}
	badID := strings.Repeat("b", 32) + ".wav"
	if err = os.Symlink(outside, filepath.Join(work.BotDir(root, "ada"), "voice", badID)); err != nil {
		t.Fatal(err)
	}
	code, _, _ := voiceRequest(t, "GET", ui.URL+"/api/bots/ada/voice/"+badID, cookie, nil, nil)
	if code != 404 {
		t.Fatalf("outside symlink GET: HTTP %d", code)
	}
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, fmt.Sprintf(`{"audios":[%q]}`, badID))
	if res.StatusCode != 400 {
		t.Fatalf("outside symlink attachment: HTTP %d", res.StatusCode)
	}
}

func TestVoiceHTTPRejectsCombinedSizeBeforeModel(t *testing.T) {
	_, ui, cookie, shots := pasteTestServer(t, false)
	defer ui.Close()
	large := append(voiceWAV(), make([]byte, 6<<20)...)
	first := uploadVoice(t, ui.URL, cookie, large)
	large[len(large)-1] = 1
	second := uploadVoice(t, ui.URL, cookie, large)
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, fmt.Sprintf(`{"audios":[%q,%q]}`, first, second))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("combined > 10 MiB: HTTP %d", res.StatusCode)
	}
	if len(shots()) != 0 {
		t.Fatal("invalid combined attachment reached the model")
	}
}
