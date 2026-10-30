package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Audio uses an already configured Completions-compatible endpoint and transport.
// It never forwards audio into chat or turns a speech file into a chat message.
type Audio struct{ Endpoint Endpoint }

func (a Audio) request(ctx context.Context, path, contentType string, body io.Reader, limit int) ([]byte, error) {
	ep := a.Endpoint
	if ep.BaseURL == "" || ep.APIKey == "" || (ep.Protocol != "" && ep.Protocol != ChatCompletions) {
		return nil, errors.New("audio: no compatible audio backend configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(ep.BaseURL, "/")+path, body)
	if err != nil {
		return nil, errors.New("audio: invalid backend endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+ep.APIKey)
	req.Header.Set("Content-Type", contentType)
	client := ep.HTTPClient
	if client == nil {
		client = defaultHTTP
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("audio: backend request failed")
	}
	defer res.Body.Close()
	// Upstream bodies can echo credentials. Report status, never the raw body.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("audio: backend returned HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, int64(limit+1)))
	if err != nil {
		return nil, errors.New("audio: backend response interrupted")
	}
	if len(data) == 0 || len(data) > limit {
		return nil, errors.New("audio: empty or oversized backend response")
	}
	return data, nil
}

func (a Audio) Listen(ctx context.Context, name, model string, data []byte) (string, error) {
	if model == "" {
		model = "whisper-1"
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("model", model)
	_ = form.WriteField("response_format", "json")
	file, err := form.CreateFormFile("file", name)
	if err != nil {
		return "", err
	}
	_, _ = file.Write(data)
	_ = form.Close()
	raw, err := a.request(ctx, "/audio/transcriptions", form.FormDataContentType(), &body, 1<<20)
	if err != nil {
		return "", err
	}
	var out struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &out) != nil || strings.TrimSpace(out.Text) == "" {
		return "", errors.New("audio: backend returned no transcription")
	}
	return out.Text, nil
}

func (a Audio) Speak(ctx context.Context, text, model, voice string) ([]byte, error) {
	if model == "" {
		model = "tts-1"
	}
	if voice == "" {
		voice = "alloy"
	}
	raw, _ := json.Marshal(map[string]string{"model": model, "voice": voice, "input": text, "response_format": "mp3"})
	return a.request(ctx, "/audio/speech", "application/json", bytes.NewReader(raw), 10<<20)
}
