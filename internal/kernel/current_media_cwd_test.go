package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xbot/internal/provider"
	"xbot/internal/work"
)

func TestMediaCwdFallbackAndScopedInheritance(t *testing.T) {
	for _, name := range []string{"deleted-foreground", "deleted-child", "missing-child"} {
		t.Run(name, func(t *testing.T) {
			k, dir, _ := outImageKernel(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			photo, clip := onePixelPNG(t), audioWAV()
			if err := os.WriteFile(filepath.Join(dir, "photo.png"), photo, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "clip.wav"), clip, 0600); err != nil {
				t.Fatal(err)
			}
			if name == "missing-child" {
				foreground := filepath.Join(dir, "foreground")
				if err := os.Mkdir(foreground, 0700); err != nil {
					t.Fatal(err)
				}
				photo = sizedPNG(t, 2, 2)
				clip = bytes.Clone(clip)
				clip[len(clip)-1] = 7
				if err := os.WriteFile(filepath.Join(foreground, "photo.png"), photo, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(foreground, "clip.wav"), clip, 0600); err != nil {
					t.Fatal(err)
				}
				k.storeCwd(foreground)
				ctx = context.WithValue(ctx, cwdScopeKey{}, "child")
			} else {
				deleted := filepath.Join(dir, "deleted")
				if err := os.Mkdir(deleted, 0700); err != nil {
					t.Fatal(err)
				}
				if name == "deleted-child" {
					k.storeCwd(dir)
					k.storeScope("child", deleted)
					ctx = context.WithValue(ctx, cwdScopeKey{}, "child")
				} else {
					k.storeCwd(deleted)
				}
				if err := os.Remove(deleted); err != nil {
					t.Fatal(err)
				}
			}
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/audio/transcriptions" {
					t.Error("unexpected audio path", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				f, _, err := r.FormFile("file")
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				defer f.Close()
				got, err := io.ReadAll(f)
				if err != nil || !bytes.Equal(got, clip) {
					t.Error("listen used a different cwd", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"text":"heard"}`)
			}))
			defer backend.Close()
			k.Provider = &provider.Client{BaseURL: backend.URL + "/v1", APIKey: "fixture", Model: "fixture", HTTPClient: backend.Client()}
			k.turn = &turn{}
			ids, err := k.attachOutImages(ctx, []string{"photo.png"}, []string{"clip.wav"})
			if err != nil || len(ids) != 1 {
				t.Fatalf("attachments from effective cwd: %v %v", ids, err)
			}
			if got, err := work.ReadPasted(dir, ids[0]); err != nil || !bytes.Equal(got, photo) {
				t.Fatal("image used a different cwd", err)
			}
			_, ext := work.AudioType(clip)
			voice := work.VoiceID(clip, ext)
			if len(k.turn.outAudios) != 1 || k.turn.outAudios[0] != voice {
				t.Fatal("audio attachment used a different cwd", k.turn.outAudios)
			}
			if name == "missing-child" {
				if got := k.readScope("child"); got != filepath.Join(dir, "foreground") {
					t.Fatal("missing child did not inherit foreground", got)
				}
				// The inherited scope is a snapshot, even after foreground moves.
				k.storeCwd(dir)
			}
			if got, err := (listenTool{k}).Run(ctx, json.RawMessage(`{"path":"clip.wav"}`)); err != nil || got != "heard" {
				t.Fatal("listen from effective cwd", got, err)
			}
		})
	}
}
