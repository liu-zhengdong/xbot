package link

import (
	"path/filepath"
	"regexp"
	"testing"
)

func TestStoreAppsCurated(t *testing.T) {
	apps, err := StoreApps()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]App{}
	for _, a := range apps {
		got[a.ID] = a
	}
	for _, id := range []string{"filesystem", "localhttp", "webhook", "watch", "skills", "mcp", "coding-agent"} {
		if _, ok := got[id]; ok {
			t.Fatalf("%s listed in the store", id)
		}
	}
	if got["desktop"].Title != "电脑操作" || got["browser"].Title != "浏览器" {
		t.Fatalf("%+v %+v", got["desktop"], got["browser"])
	}
	for _, id := range []string{"gmail", "gdrive", "gcalendar"} {
		if got[id].Kind != AppSoon {
			t.Fatalf("%s %+v", id, got[id])
		}
	}
	if e, _ := Lookup("desktop"); e.Title != "电脑操作" {
		t.Fatalf("catalog title %q", e.Title)
	}
	banned := regexp.MustCompile(`(?i)MCP|stdio|npx|uvx|command|args|JSON|桌面`)
	for _, a := range apps {
		if banned.MatchString(a.Title + a.Summary) {
			t.Fatalf("engineering word in %+v", a)
		}
	}
}

func TestLoadMCPAllBotEntryWinsAndEnableIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	global, own := filepath.Join(dir, "mcp.json"), filepath.Join(dir, "bot-mcp.json")
	for i := 0; i < 2; i++ {
		if err := EnableConnector(global, "fetch"); err != nil {
			t.Fatal(err)
		}
	}
	if err := AddMCP(own, MCPServer{Name: "fetch", Command: "/bin/echo"}); err != nil {
		t.Fatal(err)
	}
	if err := AddMCP(own, MCPServer{Name: "mine", Command: "/bin/echo"}); err != nil {
		t.Fatal(err)
	}
	list, err := LoadMCPAll(global, own)
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %v", list, err)
	}
	if list[0].Name != "fetch" || list[0].Command != "/bin/echo" {
		t.Fatalf("bot entry must override: %+v", list[0])
	}
	if list, err := LoadMCPAll(filepath.Join(dir, "none.json")); err != nil || list == nil || len(list) != 0 {
		t.Fatalf("%v %v", list, err)
	}
}

func TestResolveApp(t *testing.T) {
	for in, want := range map[string]string{"电脑操作": "desktop", "浏览器": "browser", "网页抓取": "mcp:fetch", "Gmail": "Gmail", "webhook": "webhook"} {
		if got := ResolveApp(in); got != want {
			t.Fatalf("%q -> %q, want %q", in, got, want)
		}
	}
}

// A catalog row that names a key goes through the chat card; rows without one stay a one-click add.
func TestStoreKeyRowsNeedSecret(t *testing.T) {
	c, err := LookupConnector("context7")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.SecretEnvs) != 1 || c.SecretEnvs[0] != "CONTEXT7_API_KEY" {
		t.Fatalf("context7 keys %v", c.SecretEnvs)
	}
	apps, err := StoreApps()
	if err != nil {
		t.Fatal(err)
	}
	need := map[string]bool{}
	for _, a := range apps {
		need[a.ID] = a.NeedsSecret
	}
	for id, want := range map[string]bool{"context7": true, "telegram": true, "lark": true, "fetch": false, "playwright": false, "desktop": false, "browser": false} {
		if need[id] != want {
			t.Fatalf("%s NeedsSecret=%v, want %v", id, need[id], want)
		}
	}
}
