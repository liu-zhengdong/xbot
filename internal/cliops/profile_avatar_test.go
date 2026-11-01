package cliops

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"xbot/internal/bot"
)

func profileAvatarPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	state := uint32(17)
	for i := 0; i < len(img.Pix); i += 4 {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(state), byte(state>>8), byte(state>>16), 255
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if encoded.Len() <= maxBody {
		t.Fatal("fixture must exceed the JSON metadata limit")
	}
	return encoded.Bytes()
}

func TestProfileAvatarRawUploadClearAndSniff(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "fixture persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, originalSeed := b.AvatarState()
	server, err := Start(root, Handlers{ProfileAvatar: func(data []byte, clear bool) (Profile, error) {
		result, err := b.ApplyAvatarChange(data, clear)
		avatar, seed := b.AvatarState()
		return Profile{Name: result.Name, PreviousName: result.PreviousName, Avatar: avatar, AvatarSeed: seed, Scope: "runtime", Applied: len(result.AppliedFields) != 0, AppliedFields: result.AppliedFields}, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	data := profileAvatarPNG(t)
	value, err := CallAvatar(context.Background(), server.Endpoint, root, bytes.NewReader(data), false)
	if err != nil || value.Avatar != bot.AvatarCustom || !reflect.DeepEqual(value.AppliedFields, []string{"avatar_file", "avatar_mime", "avatar"}) {
		t.Fatalf("upload=%+v error=%v", value, err)
	}
	stored, mime, custom, err := b.AvatarBytes()
	if err != nil || !custom || mime != "image/png" || !bytes.Equal(stored, data) {
		t.Fatal("raw image changed in transport or storage", err)
	}
	canonical, _ := CanonicalRoot(root)
	request := func(body []byte, contentType string) (Profile, error) {
		req, err := http.NewRequest(http.MethodPost, server.Endpoint+"/v1/profile/avatar", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-XBot-Root", canonical)
		req.Header.Set("Content-Type", contentType)
		return callProfile(req)
	}
	// Neither a supported-but-wrong MIME header nor a path/JSON envelope is
	// authoritative. Only the original bytes reach the image-sniffing domain.
	if _, err := request(data, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if _, err := request([]byte(`{"file":"/server/avatar.png","avatar":"data:image/png;base64,ignored"}`), "image/png"); err == nil {
		t.Fatal("JSON/path envelope accepted as an image")
	}
	stored, mime, custom, err = b.AvatarBytes()
	if err != nil || !custom || mime != "image/png" || !bytes.Equal(stored, data) {
		t.Fatal("invalid upload changed the avatar", err)
	}
	value, err = CallAvatar(context.Background(), server.Endpoint, root, nil, true)
	if err != nil || value.Avatar != bot.AvatarGenerated || value.AvatarSeed != originalSeed || !reflect.DeepEqual(value.AppliedFields, []string{"avatar"}) {
		t.Fatalf("clear=%+v error=%v", value, err)
	}
}

type avatarCountingReader struct{ read atomic.Int64 }

func (r *avatarCountingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.read.Add(int64(len(p)))
	return len(p), nil
}

func TestProfileAvatarStreamLimitAndSourceGuards(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	server, err := Start(root, Handlers{ProfileAvatar: func(data []byte, clear bool) (Profile, error) {
		calls.Add(1)
		if clear || len(data) != MaxAvatarBody {
			t.Error("unexpected admitted payload")
		}
		return Profile{Name: "ada", Scope: "runtime", Applied: true}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := CallAvatar(context.Background(), server.Endpoint, root, bytes.NewReader(make([]byte, MaxAvatarBody)), false); err != nil {
		t.Fatal("exactly 1 MiB rejected", err)
	}
	reader := &avatarCountingReader{}
	if _, err := CallAvatar(context.Background(), server.Endpoint, root, reader, false); err == nil {
		t.Fatal("oversized streaming upload accepted")
	}
	if reader.read.Load() > MaxAvatarBody+1 {
		t.Fatal("client read beyond its upload bound", reader.read.Load())
	}
	if _, err := CallAvatar(context.Background(), server.Endpoint, t.TempDir(), nil, true); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatal("wrong root reached avatar callback", err)
	}
	canonical, _ := CanonicalRoot(root)
	for _, kind := range []string{"browser", "host", "clear-body"} {
		req, err := http.NewRequest(http.MethodDelete, server.Endpoint+"/v1/profile/avatar", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-XBot-Root", canonical)
		switch kind {
		case "browser":
			req.Header.Set("Origin", "https://example.test")
		case "host":
			req.Host = "localhost:1"
		case "clear-body":
			req.Body = io.NopCloser(strings.NewReader("unexpected"))
			req.ContentLength = int64(len("unexpected"))
		}
		if _, err := callProfile(req); err == nil {
			t.Fatal("source guard accepted", kind)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("rejected input reached mutation", calls.Load())
	}
}

func TestProfileAvatarPartialAndResponseBudget(t *testing.T) {
	root := t.TempDir()
	var oversized atomic.Bool
	server, err := Start(root, Handlers{ProfileAvatar: func([]byte, bool) (Profile, error) {
		if oversized.Load() {
			return Profile{Name: "ada", Description: strings.Repeat("x", maxBody)}, nil
		}
		return Profile{Name: "ada", Avatar: bot.AvatarGenerated, Scope: "runtime", Applied: true, AppliedFields: []string{"avatar_file"}}, errors.New("fixture: MIME write failed after image write")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	value, err := CallAvatar(context.Background(), server.Endpoint, root, strings.NewReader("fixture raw bytes"), false)
	if err == nil || value.Name != "ada" || value.Avatar != bot.AvatarGenerated || value.Error == "" || !reflect.DeepEqual(value.AppliedFields, []string{"avatar_file"}) {
		t.Fatalf("partial=%+v error=%v", value, err)
	}
	oversized.Store(true)
	if value, err := CallAvatar(context.Background(), server.Endpoint, root, nil, true); err == nil || !strings.Contains(err.Error(), "response too large") || value.Name != "" {
		t.Fatalf("oversized metadata=%+v error=%v", value, err)
	}
	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, strings.Repeat("x", maxBody+1)) }))
	defer invalid.Close()
	if _, err := CallAvatar(context.Background(), invalid.URL, root, nil, true); err == nil || !strings.Contains(err.Error(), "invalid runtime response") {
		t.Fatal("client lost the JSON response budget", err)
	}
}
