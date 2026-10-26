package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/work"
)

func TestResponsesImageGenerationLargeCompleted(t *testing.T) {
	// Two actual 13 MiB payloads make the final SSE line exceed the old
	// 32 MiB scanner cap, even though each individual done event fits.
	png := fakePNG(13 << 20)
	b64 := base64.StdEncoding.EncodeToString(png)
	items := make([]string, 2)
	for i := range items {
		items[i] = fmt.Sprintf(`{"type":"image_generation_call","id":"ig_large_%d","status":"completed","result":"%s"}`, i, b64)
	}
	completed := `data: {"type":"response.completed","response":{"status":"completed","output":[` + strings.Join(items, ",") + "]}}\n\n"
	if len(completed) <= 32<<20 {
		t.Fatalf("fixture must exceed the old cap: %d", len(completed))
	}
	for _, doneFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("done_first=%t", doneFirst), func(t *testing.T) {
			dir := t.TempDir()
			var paths []string
			ctx := WithImageGen(context.Background(), ImageGen{Save: func(data []byte) (string, error) {
				path, err := work.WriteGenerated(dir, data)
				if err == nil {
					paths = append(paths, path)
				}
				return path, err
			}})
			var readers []io.Reader
			if doneFirst {
				for i, item := range items {
					done := fmt.Sprintf("data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":%s}\n\n", i, item)
					if len(done) >= 32<<20 {
						t.Fatal("individual done event must fit the old cap")
					}
					readers = append(readers, strings.NewReader(done))
				}
			}
			readers = append(readers, strings.NewReader(completed))
			start := time.Now()
			msg, err := readResponses(ctx, io.MultiReader(readers...), nil)
			if err != nil {
				t.Fatalf("completed line %d bytes, already saved %d images: %v", len(completed), len(paths), err)
			}
			if len(paths) != 2 || paths[0] == paths[1] || len(msg.Content) != 3 {
				t.Fatalf("want two unique saves, hosted parts and visible receipt: paths=%v parts=%d", paths, len(msg.Content))
			}
			files, err := os.ReadDir(filepath.Join(dir, work.GeneratedDir))
			if err != nil || len(files) != 2 {
				t.Fatalf("generated files=%d err=%v", len(files), err)
			}
			for i, path := range paths {
				data, err := os.ReadFile(filepath.Join(dir, path))
				if err != nil || !bytes.Equal(data, png) {
					t.Fatalf("saved image %d differs: bytes=%d err=%v", i, len(data), err)
				}
				p := msg.Content[i]
				if p.Hosted == nil || p.Hosted.Status != "completed" || !strings.Contains(p.Text, path) || len(p.VendorRaw) != 0 {
					t.Fatalf("final part %d lost its saved path: %+v", i, p)
				}
			}
			input, err := responseInput([]Message{msg})
			if err != nil {
				t.Fatal(err)
			}
			replay, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				if !bytes.Contains(replay, []byte(path)) || !strings.Contains(msg.Text(), path) {
					t.Fatalf("model replay lost %s", path)
				}
			}
			if len(replay) > 1<<20 || bytes.Contains(replay, []byte(b64[:64])) || len(msg.Images) != 0 {
				t.Fatal("large image bytes reached replay or automatic attachments")
			}
			t.Logf("completed line=%d bytes, saved=%d x %d bytes, parse/save=%s", len(completed), len(paths), len(png), time.Since(start))
		})
	}
}

func TestResponsesImageGenerationLineBudget(t *testing.T) {
	encoded := maxImagesPerResponse * base64.StdEncoding.EncodedLen(work.GeneratedMaxBytes)
	if imageStreamLine < encoded+(1<<20) || imageStreamLine > encoded+(2<<20) {
		t.Fatalf("line budget %d must cover eight max-size results plus 1–2 MiB overhead (results=%d)", imageStreamLine, encoded)
	}
}
