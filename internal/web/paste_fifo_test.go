//go:build unix

package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"xbot/internal/work"
)

// TestPasteRefusesSpecialFiles covers a name swapped for a FIFO: opening one
// waits for a writer, so a request would hang instead of being refused. The
// file is opened without blocking and the type comes from the same handle.
func TestPasteRefusesSpecialFiles(t *testing.T) {
	root, ui, cookie, _ := pasteTestServer(t, true)
	defer ui.Close()

	dir := work.PastedDirFor(work.BotDir(root, "ada"))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("ef", 16) + ".png"
	fifo := filepath.Join(dir, id)
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}

	done := make(chan int, 1)
	go func() {
		code, _ := getStatus(t, ui.URL+"/api/bots/ada/paste/"+id, cookie)
		done <- code
	}()
	select {
	case code := <-done:
		if code != http.StatusNotFound {
			t.Errorf("GET on a FIFO: %d, want 404", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GET on a FIFO hung instead of being refused")
	}

	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, fmt.Sprintf(`{"images":[%q]}`, id))
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("message naming a FIFO: %d, want 400", res.StatusCode)
	}
}
