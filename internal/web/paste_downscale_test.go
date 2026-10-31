package web

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasteStoresFittedImage(t *testing.T) {
	root, ui, cookie, _ := pasteTestServer(t, true)
	defer ui.Close()
	for _, format := range []string{"png", "jpeg", "gif"} {
		for _, size := range []image.Point{{2340, 1682}, {1682, 2340}, {32, 24}} {
			t.Run(format+"/"+size.String(), func(t *testing.T) {
				var buf bytes.Buffer
				src := image.NewGray(image.Rect(0, 0, size.X, size.Y))
				var err error
				switch format {
				case "png":
					err = png.Encode(&buf, src)
				case "jpeg":
					err = jpeg.Encode(&buf, src, nil)
				case "gif":
					err = gif.Encode(&buf, src, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				id := uploadPaste(t, ui.URL, cookie, buf.Bytes())
				stored, err := os.ReadFile(storedPastePath(root, "ada", id))
				if err != nil {
					t.Fatal(err)
				}
				cfg, gotFormat, err := image.DecodeConfig(bytes.NewReader(stored))
				if err != nil {
					t.Fatal(err)
				}
				want := size
				wantFormat := format
				if size.X > 2048 || size.Y > 2048 {
					if size.X > size.Y {
						want = image.Pt(2048, size.Y*2048/size.X)
					} else {
						want = image.Pt(size.X*2048/size.Y, 2048)
					}
					if format == "gif" {
						wantFormat = "png"
					}
				} else if !bytes.Equal(stored, buf.Bytes()) {
					t.Fatal("compliant image changed")
				}
				if cfg.Width != want.X || cfg.Height != want.Y || gotFormat != wantFormat || !strings.HasSuffix(id, "."+map[string]string{"png": "png", "jpeg": "jpg", "gif": "gif"}[wantFormat]) {
					t.Fatalf("stored %s %dx%d as %s, want %s %v", gotFormat, cfg.Width, cfg.Height, id, wantFormat, want)
				}
				if got := getAuth(t, ui.URL+"/api/bots/ada/paste/"+id, cookie); !bytes.Equal(got, stored) {
					t.Fatal("GET differs from fitted disk bytes")
				}
			})
		}
	}
}

func TestPasteRefusesOversizeWebP(t *testing.T) {
	root, ui, cookie, _ := pasteTestServer(t, true)
	defer ui.Close()
	// VP8X dimension header, matching view_image's metadata-only WebP path.
	data := make([]byte, 30)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 22)
	copy(data[8:16], "WEBPVP8X")
	binary.LittleEndian.PutUint32(data[16:20], 10)
	data[24], data[25] = byte(2339&255), byte(2339>>8)
	data[27], data[28] = byte(1681&255), byte(1681>>8)
	req, _ := http.NewRequest(http.MethodPost, ui.URL+"/api/bots/ada/paste", bytes.NewReader(data))
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 400 || !bytes.Contains(raw, []byte("WebP")) || !bytes.Contains(raw, []byte("无法缩小")) {
		t.Fatalf("%d %s", res.StatusCode, raw)
	}
	files, err := os.ReadDir(filepath.Dir(storedPastePath(root, "ada", "unused")))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("refused image left a file")
	}
}
