package work

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const VoiceDir = "voice"
const VoiceMaxBytes = 10 << 20
const VoiceMaxPerMessage = 4

// AudioType recognizes the containers supported by browser recording and playback.
func AudioType(data []byte) (mime, ext string) {
	switch {
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		return "audio/wav", ".wav"
	case len(data) >= 4 && string(data[:4]) == "OggS":
		return "audio/ogg", ".ogg"
	case len(data) >= 4 && string(data[:4]) == "\x1a\x45\xdf\xa3":
		return "audio/webm", ".webm"
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		return "audio/mp4", ".m4a"
	case len(data) >= 3 && string(data[:3]) == "ID3", len(data) >= 2 && data[0] == 0xff && data[1]&0xe0 == 0xe0:
		return "audio/mpeg", ".mp3"
	case len(data) >= 4 && string(data[:4]) == "fLaC":
		return "audio/flac", ".flac"
	}
	return "", ""
}

func ValidVoiceID(id string) bool {
	ext := filepath.Ext(id)
	if !strings.Contains("|.wav|.ogg|.webm|.m4a|.mp3|.flac|", "|"+ext+"|") || ext == "" {
		return false
	}
	base := strings.TrimSuffix(id, ext)
	if len(base) != 32 {
		return false
	}
	_, err := hex.DecodeString(base)
	return err == nil
}

// StoreVoice stores immutable, content-addressed audio inside the Bot workspace.
func StoreVoice(dir string, data []byte) (string, error) {
	_, ext := AudioType(data)
	if ext == "" || len(data) > VoiceMaxBytes {
		return "", errors.New("audio: unsupported or larger than 10 MiB")
	}
	id := VoiceID(data, ext)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err = root.MkdirAll(VoiceDir, 0700); err != nil {
		return "", err
	}
	err = root.WriteFile(VoiceDir+"/"+id, data, 0600)
	return id, err
}

// OpenVoice preserves range requests without reading the entire recording.
func OpenVoice(dir, id string) (*os.File, error) {
	if !ValidVoiceID(id) {
		return nil, os.ErrNotExist
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(VoiceDir+"/"+id, os.O_RDONLY|nonBlockOpen, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > VoiceMaxBytes {
		f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

// ReadAudioFile reads a local audio path through its workspace boundary.
func ReadAudioFile(dir, path string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(path, os.O_RDONLY|nonBlockOpen, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > VoiceMaxBytes {
		return nil, errors.New("audio: not a regular file or larger than 10 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, VoiceMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if mime, _ := AudioType(data); mime == "" || len(data) > VoiceMaxBytes {
		return nil, errors.New("audio: unsupported or larger than 10 MiB")
	}
	return data, nil
}

func VoiceID(data []byte, ext string) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16]) + ext
}
