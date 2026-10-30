package kernel

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/work"
)

func audioWAV() []byte {
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

func TestAudioToolsListenThenTTSSavesWithoutSending(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	data := audioWAV()
	speechCalls, listenCalls := 0, 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake-audio-key" {
			t.Error("configured backend credential not used")
		}
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			listenCalls++
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if r.FormValue("model") != "whisper-1" {
				t.Errorf("transcription model = %q", r.FormValue("model"))
			}
			f, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			raw, _ := io.ReadAll(f)
			if !bytes.Equal(raw, data) {
				t.Error("listen sent different audio bytes")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"text":"今晚七点开会"}`)
		case "/v1/audio/speech":
			speechCalls++
			var args map[string]string
			if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
				t.Error(err)
			}
			if args["input"] != "收到，七点见。" || args["model"] != "tts-1" || args["voice"] != "alloy" {
				t.Errorf("speech args = %v", args)
			}
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write(data)
		default:
			t.Errorf("unexpected backend call %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer backend.Close()
	k.Provider = &provider.Client{BaseURL: backend.URL + "/v1", APIKey: "fake-audio-key", Model: "mock", HTTPClient: backend.Client()}
	id, err := work.StoreVoice(dir, data)
	if err != nil {
		t.Fatal(err)
	}
	before, err := k.Bot.Count()
	if err != nil {
		t.Fatal(err)
	}
	listened, err := (listenTool{k}).Run(context.Background(), json.RawMessage(fmt.Sprintf(`{"path":%q}`, "voice/"+id)))
	if err != nil || listened != "今晚七点开会" {
		t.Fatalf("listen = %q %v", listened, err)
	}
	saved, err := (ttsTool{k}).Run(context.Background(), json.RawMessage(`{"text":"收到，七点见。"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, "voice/"+id) {
		t.Fatalf("TTS omitted actual saved path: %q", saved)
	}
	count, err := k.Bot.Count()
	if err != nil || count != before {
		t.Fatalf("listen/TTS unexpectedly appended a message: %d -> %d %v", before, count, err)
	}
	if speechCalls != 1 || listenCalls != 1 {
		t.Fatalf("audio requests: listen=%d speech=%d", listenCalls, speechCalls)
	}
	if k.turn != nil && len(k.turn.outAudios) > 0 {
		t.Fatalf("TTS queued a bubble: %v", k.turn.outAudios)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "voice", id))
	if err != nil || !bytes.Equal(raw, data) {
		t.Fatalf("TTS file not readable: %v", err)
	}
}

func TestAudioSendMessageAttachesOnlyThroughReply(t *testing.T) {
	k, dir, p := outImageKernel(t)
	data := audioWAV()
	if err := os.WriteFile(filepath.Join(dir, "reply.wav"), data, 0600); err != nil {
		t.Fatal(err)
	}
	p.resps = []provider.Message{
		toolCall("audio-send", "send_message_to_user", `{"audios":["reply.wav","reply.wav"]}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("后续一句")},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "请语音回复", time.Unix(10, 0))); err != nil {
		t.Fatal(err)
	}
	id := work.VoiceID(data, ".wav")
	found := false
	for _, e := range entriesFor(t, k.Bot) {
		if len(e.Audios) == 0 {
			continue
		}
		if e.Message.Role != provider.RoleAssistant || len(e.Message.Calls()) != 0 || !slices.Equal(e.Audios, []string{id}) {
			t.Fatalf("audio on wrong transcript line: %+v", e)
		}
		found = true
	}
	if !found {
		t.Fatal("pure audio send never reached reply attachment")
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "再说一句", time.Unix(11, 0))); err != nil {
		t.Fatal(err)
	}
	last := p.reqs[len(p.reqs)-1]
	seen := false
	for _, m := range last.Messages {
		if strings.Contains(m.Text(), "voice/"+id) {
			seen = true
		}
	}
	if !seen {
		t.Fatal("later Agent context lost audio reply path")
	}
	n, err := k.Bot.Count()
	if err != nil {
		t.Fatal(err)
	}
	expanded, err := k.Expand(1, n)
	if err != nil || !strings.Contains(expanded, "voice/"+id) {
		t.Fatalf("history lost playable file path: %q %v", expanded, err)
	}
}

func TestAudioLateSendStaysWithOriginatingReply(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	if err := os.WriteFile(filepath.Join(dir, "late.wav"), audioWAV(), 0600); err != nil {
		t.Fatal(err)
	}
	original := &turn{turnSource: link.NameWeb}
	current := &turn{turnSource: link.NameWeb}
	k.turn = current
	ctx := context.WithValue(context.Background(), callTurnKey{}, original)
	ctx = context.WithValue(ctx, callReplyGenKey{}, 0)
	args := json.RawMessage(`{"audios":["late.wav"]}`)
	if _, err := (sendMessageTool{k}).Run(ctx, args); err != nil {
		t.Fatal(err)
	}
	if len(original.outAudios) != 1 || len(current.outAudios) != 0 {
		t.Fatalf("attachment crossed turns: original=%v current=%v", original.outAudios, current.outAudios)
	}
	original.replyGen++
	if _, err := (sendMessageTool{k}).Run(ctx, args); err == nil {
		t.Fatal("already closed reply accepted late audio")
	}
	if len(current.outAudios) != 0 {
		t.Fatal("late audio leaked into current turn")
	}
}

func TestAudioBadMixedAttachmentQueuesNothing(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	k.turn = &turn{turnSource: link.NameWeb}
	if err := os.WriteFile(filepath.Join(dir, "good.png"), onePixelPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (sendMessageTool{k}).Run(context.Background(), json.RawMessage(`{"text":"看看","images":["good.png"],"audios":["missing.wav"]}`))
	if err == nil {
		t.Fatal("missing audio accepted")
	}
	if len(k.turn.outImages) != 0 || len(k.turn.outAudios) != 0 {
		t.Fatalf("failed send left hidden attachments on next reply: images=%v audios=%v", k.turn.outImages, k.turn.outAudios)
	}
}

func TestAudioToolsFailHonestlyWithoutBackend(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	if err := os.WriteFile(filepath.Join(dir, "input.wav"), audioWAV(), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := (listenTool{k}).Run(context.Background(), json.RawMessage(`{"path":"input.wav"}`)); err == nil || out != "" {
		t.Fatalf("listen without backend = %q %v", out, err)
	}
	if out, err := (ttsTool{k}).Run(context.Background(), json.RawMessage(`{"text":"你好"}`)); err == nil || out != "" {
		t.Fatalf("TTS without backend = %q %v", out, err)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "voice")); err == nil && len(entries) > 0 {
		t.Fatalf("failed backend left audio files: %v", entries)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "fake-audio-key upstream echo", 403) }))
	defer backend.Close()
	k.Provider = &provider.Client{BaseURL: backend.URL, APIKey: "fake-audio-key", Model: "mock", HTTPClient: backend.Client()}
	if out, err := (ttsTool{k}).Run(context.Background(), json.RawMessage(`{"text":"你好"}`)); err == nil || out != "" || !strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "fake-audio-key") {
		t.Fatalf("TTS backend rejection = %q %v", out, err)
	}
}

func TestAudioListenRejectsOutsideWorkspaceBeforeBackend(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	outside := filepath.Join(t.TempDir(), "outside.wav")
	if err := os.WriteFile(outside, audioWAV(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.wav")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"text":"outside"}`) }))
	defer backend.Close()
	k.Provider = &provider.Client{BaseURL: backend.URL, APIKey: "fake", Model: "mock", HTTPClient: backend.Client()}
	for _, path := range []string{outside, "escape.wav"} {
		args, _ := json.Marshal(map[string]string{"path": path})
		if out, err := (listenTool{k}).Run(context.Background(), args); err == nil || out != "" {
			t.Fatalf("outside listen = %q %v", out, err)
		}
	}
	if calls != 0 {
		t.Fatal("outside audio was sent to backend")
	}
}
