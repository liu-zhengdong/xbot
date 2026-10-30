package web

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"xbot/internal/bot"
	"xbot/internal/work"
)

func (s *Server) postVoice(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, work.VoiceMaxBytes+1))
	if err != nil {
		writeErr(w, 400, "语音没传上来")
		return
	}
	if len(data) > work.VoiceMaxBytes {
		writeErr(w, 413, "语音超过 10 MB")
		return
	}
	if mime, _ := work.AudioType(data); mime == "" {
		writeErr(w, 400, "无法读取这段语音")
		return
	}
	id, err := work.StoreVoice(work.BotDir(s.Root, b.Name()), data)
	if err != nil {
		writeErr(w, 500, "语音保存失败")
		return
	}
	writeJSON(w, 200, map[string]string{"audio": id})
}

func (s *Server) getVoice(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	f, err := work.OpenVoice(work.BotDir(s.Root, b.Name()), r.PathValue("audio"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	var header [512]byte
	n, _ := f.Read(header[:])
	mime, _ := work.AudioType(header[:n])
	if mime == "" {
		http.NotFound(w, r)
		return
	}
	_, _ = f.Seek(0, io.SeekStart)
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, r.PathValue("audio"), time.Time{}, f)
}

func (s *Server) checkVoice(b *bot.Bot, ids []string) ([]string, error) {
	if len(ids) > work.VoiceMaxPerMessage {
		return nil, fmt.Errorf("一次最多 %d 段语音", work.VoiceMaxPerMessage)
	}
	var out []string
	total := int64(0)
	for _, id := range ids {
		if slices.Contains(out, id) {
			continue
		}
		f, err := work.OpenVoice(work.BotDir(s.Root, b.Name()), id)
		if err != nil {
			return nil, fmt.Errorf("语音已失效，请重新录音")
		}
		st, _ := f.Stat()
		var header bytes.Buffer
		_, err = io.CopyN(&header, f, min(st.Size(), 512))
		f.Close()
		if mime, _ := work.AudioType(header.Bytes()); err != nil || mime == "" {
			return nil, fmt.Errorf("无法读取这段语音")
		}
		total += st.Size()
		if total > work.VoiceMaxBytes {
			return nil, fmt.Errorf("语音合计超过 10 MB")
		}
		out = append(out, id)
	}
	return out, nil
}
