package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"xbot/internal/bot"
)

func TestProfileRuntimeReservedFolderPreflight(t *testing.T) {
	root, call := profileRuntimeWeb(t)
	config := filepath.Join(bot.Dir(root, "old"), "config.json")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"group": bot.UngroupedName, "description": "must not save", "persona": "must not save"})
	status, raw := call(http.MethodPatch, "/api/bots/old", bytes.NewReader(body), "application/json")
	if status != http.StatusBadRequest {
		t.Fatalf("reserved folder changed existing status contract: %d %s", status, raw)
	}
	after, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("reserved folder applied profile fields")
	}
}
