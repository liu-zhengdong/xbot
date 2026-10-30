//go:build smoke

package smoke

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
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/work"
)

// Real xbot serve, HTTP upload, tools door, audio HTTP APIs, final reply
// attachment, transcript and ranged playback. Only chat and speech are scripted.
func TestVoiceChatBinary(t *testing.T) {
	wav := make([]byte, 44+1600)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 8000)
	binary.LittleEndian.PutUint32(wav[28:], 16000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], 1600)
	answer := bytes.Clone(wav)
	answer[len(answer)-1] = 1
	var calls, heard, spoken atomic.Int32
	var uploaded atomic.Value
	uploaded.Store("")
	outputID := work.VoiceID(answer, ".wav")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			heard.Add(1)
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				http.Error(w, "multipart", 400)
				return
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				http.Error(w, "file", 400)
				return
			}
			defer file.Close()
			got, _ := io.ReadAll(file)
			if !bytes.Equal(got, wav) {
				t.Error("listen did not receive the uploaded recording")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"text":"VOICE_HEARD_SEVEN"}`)
		case "/v1/audio/speech":
			spoken.Add(1)
			var in map[string]string
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			if in["input"] != "收到，七点见。" {
				t.Errorf("TTS input=%q", in["input"])
			}
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write(answer)
		case "/v1/chat/completions":
			var req struct {
				Messages json.RawMessage `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				http.Error(w, "request", 400)
				return
			}
			n := calls.Add(1)
			raw := string(req.Messages)
			id := uploaded.Load().(string)
			tool, args := "", ""
			switch n {
			case 1:
				if id == "" || !strings.Contains(raw, "voice/"+id) {
					t.Error("Agent did not see the uploaded file path")
				}
				if strings.Contains(raw, "VOICE_HEARD_SEVEN") || heard.Load() != 0 {
					t.Error("voice was transcribed before Agent requested listen")
				}
				tool, args = "tools", `{"action":"describe","id":"audio"}`
			case 2:
				arg, _ := json.Marshal(map[string]any{"action": "call", "id": "listen", "args": map[string]string{"path": "voice/" + id}})
				tool, args = "tools", string(arg)
			case 3:
				if heard.Load() != 1 || !strings.Contains(raw, "VOICE_HEARD_SEVEN") {
					t.Error("listen result did not reach Agent")
				}
				tool, args = "tools", `{"action":"call","id":"tts","args":{"text":"收到，七点见。"}}`
			case 4:
				saved := regexp.MustCompile(`voice/[a-f0-9]{32}\.wav`).FindAllString(raw, -1)
				if spoken.Load() != 1 || !strings.Contains(raw, "voice/"+outputID) || len(saved) == 0 {
					t.Error("Agent did not receive TTS's real save receipt")
				}
				arg, _ := json.Marshal(map[string]any{"text": "收到。", "audios": []string{"voice/" + outputID}})
				tool, args = "send_message_to_user", string(arg)
			case 5:
			default:
				t.Errorf("unexpected chat step %d", n)
			}
			message := map[string]any{"role": "assistant", "content": "SILENT"}
			reason := "stop"
			if tool != "" {
				message["content"] = ""
				message["tool_calls"] = []any{map[string]any{"id": fmt.Sprintf("voice-%d", n), "type": "function", "function": map[string]any{"name": tool, "arguments": args}}}
				reason = "tool_calls"
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": reason}}})
		default:
			t.Errorf("unexpected backend path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()
	srv := Start(t, &Stub{srv: backend})
	srv.CreateBot("ada", "Be brief.")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := time.Now()
	upload, err := http.NewRequestWithContext(ctx, "POST", srv.URL+"/api/bots/ada/voice", bytes.NewReader(wav))
	if err != nil {
		t.Fatal(err)
	}
	upload.Header.Set("Content-Type", "audio/wav")
	res, err := srv.client.Do(upload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("voice upload: HTTP %d %s", res.StatusCode, raw)
	}
	var media struct {
		Audio string `json:"audio"`
	}
	if err = json.Unmarshal(raw, &media); err != nil || media.Audio == "" {
		t.Fatalf("upload returned no audio: %s %v", raw, err)
	}
	uploaded.Store(media.Audio)
	code, stream := srv.do("POST", "/api/bots/ada/messages", map[string]any{"audios": []string{media.Audio}})
	if code != 200 || strings.Contains(string(stream), "event: error") || !strings.Contains(string(stream), "event: done") {
		t.Fatalf("voice message: HTTP %d %s", code, stream)
	}
	if calls.Load() != 5 || heard.Load() != 1 || spoken.Load() != 1 {
		t.Fatalf("steps: chat=%d listen=%d TTS=%d", calls.Load(), heard.Load(), spoken.Load())
	}
	code, raw = srv.do("GET", "/api/bots/ada/transcript", nil)
	if code != 200 {
		t.Fatalf("transcript HTTP %d", code)
	}
	var transcript struct {
		Messages []struct {
			Role      string   `json:"role"`
			Content   string   `json:"content"`
			Audios    []string `json:"audios"`
			ToolCalls []any    `json:"tool_calls"`
		} `json:"messages"`
	}
	if err = json.Unmarshal(raw, &transcript); err != nil {
		t.Fatal(err)
	}
	users, bots := 0, 0
	for _, row := range transcript.Messages {
		if len(row.Audios) == 0 {
			continue
		}
		if len(row.Audios) != 1 {
			t.Fatalf("unexpected audio list: %+v", row)
		}
		switch row.Role {
		case "user":
			users++
			if row.Audios[0] != media.Audio || row.Content != "" {
				t.Fatalf("owner voice changed: %+v", row)
			}
		case "assistant":
			bots++
			if row.Audios[0] != outputID || len(row.ToolCalls) != 0 {
				t.Fatalf("audio attached outside final reply: %+v", row)
			}
		default:
			t.Fatalf("audio on tool/system line: %+v", row)
		}
	}
	if users != 1 || bots != 1 {
		t.Fatalf("voice attachments user=%d bot=%d", users, bots)
	}
	for _, sample := range []struct {
		id   string
		data []byte
	}{{media.Audio, wav}, {outputID, answer}} {
		saved, err := os.ReadFile(filepath.Join(work.BotDir(srv.Root, "ada"), "voice", sample.id))
		if err != nil || !bytes.Equal(saved, sample.data) {
			t.Fatalf("workspace recording %s: %v", sample.id, err)
		}
		get, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/bots/ada/voice/"+sample.id, nil)
		if err != nil {
			t.Fatal(err)
		}
		get.Header.Set("Range", "bytes=0-43")
		playback, err := srv.client.Do(get)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(playback.Body)
		playback.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if playback.StatusCode != 206 || playback.Header.Get("Content-Type") != "audio/wav" || !bytes.Equal(data, sample.data[:44]) {
			t.Fatalf("playback %s: HTTP %d len=%d", sample.id, playback.StatusCode, len(data))
		}
	}
	t.Logf("scripted binary voice journey: %s; chat=%d listen=%d TTS=%d", time.Since(started), calls.Load(), heard.Load(), spoken.Load())
}
