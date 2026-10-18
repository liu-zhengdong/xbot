package web

import (
	"os"
	"strings"
	"testing"
)

// ADR 0012 retired nodes: /api/bots/status has no node field, so the page keeps no
// remote-folder sidebar code. Remote bots appear through the devices groups instead.
func TestNoNodeFolders(t *testing.T) {
	js, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, dead := range []string{"ensureRemoteRows", "data-remote-node", "stNodePrefix", "st.node"} {
		if strings.Contains(string(js), dead) {
			t.Errorf("app.js still has node-era code: %s", dead)
		}
	}
}
