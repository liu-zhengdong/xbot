package work

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestVoiceStorageAndBoundary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new-bot")
	data := []byte("RIFF0000WAVEfmt voice test")
	id, err := StoreVoice(dir, data)
	if err != nil {
		t.Fatal(err)
	}
	f, err := OpenVoice(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	out, err := ReadAudioFile(dir, VoiceDir+"/"+id)
	if err != nil || !bytes.Equal(data, out) {
		t.Fatalf("read %q: %v", out, err)
	}
	if _, err = OpenVoice(dir, "../"+id); err == nil {
		t.Fatal("accepted traversal")
	}
	outside := filepath.Join(t.TempDir(), "outside.wav")
	if err = os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, VoiceDir, "0123456789abcdef0123456789abcdef.wav")
	if err = os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenVoice(dir, filepath.Base(link)); err == nil {
		t.Fatal("accepted outside symlink")
	}
	if _, err = ReadAudioFile(dir, VoiceDir+"/"+filepath.Base(link)); err == nil {
		t.Fatal("read outside symlink")
	}
	if _, err = StoreVoice(dir, bytes.Repeat([]byte("x"), VoiceMaxBytes+1)); err == nil {
		t.Fatal("accepted oversized audio")
	}
	if _, err = StoreVoice(dir, []byte("<html>error page</html>")); err == nil {
		t.Fatal("accepted error page")
	}
}
