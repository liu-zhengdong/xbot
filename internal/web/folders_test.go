package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func TestSidebarFolders(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	mustFolderWebBot(t, root, "ada", "u1")
	mustFolderWebBot(t, root, "bob", "u1")
	mustFolderWebBot(t, root, "cara", "u2")
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()

	call := func(method, path, body string, auth bool) (int, []byte) {
		t.Helper()
		var rdr io.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, srv.URL+path, rdr)
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, raw
	}

	code, raw := call(http.MethodGet, "/api/groups", "", false)
	if code != http.StatusUnauthorized {
		t.Fatalf("auth %d %s", code, raw)
	}

	code, raw = call(http.MethodGet, "/api/groups", "", true)
	view := decodeFolders(t, raw)
	if code != http.StatusOK || len(view.Order) != 0 || len(view.Folders) != 1 || !view.Folders[0].Ungrouped || view.Folders[0].Count != 2 {
		t.Fatalf("empty %d %+v %s", code, view, raw)
	}
	if view.Folders[0].Bots[0] != "ada" || view.Folders[0].Bots[1] != "bob" {
		t.Fatalf("ungrouped bots %+v", view.Folders[0].Bots)
	}

	code, raw = call(http.MethodPost, "/api/groups", `{"name":"工作"}`, true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"name":"工作"`) {
		t.Fatalf("create %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/groups", `{"name":"工作"}`, true)
	if code != http.StatusConflict || !strings.Contains(string(raw), "already exists") {
		t.Fatalf("dup %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/groups", `{"name":"未分组"}`, true)
	if code != http.StatusBadRequest {
		t.Fatalf("reserved %d %s", code, raw)
	}

	code, raw = call(http.MethodPatch, "/api/bots/ada", `{"group":"工作"}`, true)
	if code != http.StatusOK {
		t.Fatalf("set ada %d %s", code, raw)
	}
	code, raw = call(http.MethodPatch, "/api/bots/bob", `{"group":"工作"}`, true)
	if code != http.StatusOK {
		t.Fatalf("set bob %d %s", code, raw)
	}
	code, raw = call(http.MethodPatch, "/api/bots/cara", `{"group":"工作"}`, true)
	if code != http.StatusNotFound {
		t.Fatalf("cara %d %s", code, raw)
	}
	code, raw = call(http.MethodPatch, "/api/bots/ada", `{"group":"没有"}`, true)
	if code != http.StatusNotFound {
		t.Fatalf("missing folder %d %s", code, raw)
	}

	code, raw = call(http.MethodGet, "/api/groups", "", true)
	view = decodeFolders(t, raw)
	if code != http.StatusOK || len(view.Order) != 1 || view.Order[0] != "工作" || view.Folders[0].Count != 2 || view.Folders[1].Count != 0 {
		t.Fatalf("filled %d %+v", code, view)
	}

	code, raw = call(http.MethodPatch, "/api/groups/"+url.PathEscape("工作"), `{"name":"工作台"}`, true)
	if code != http.StatusOK {
		t.Fatalf("rename %d %s", code, raw)
	}
	code, raw = call(http.MethodGet, "/api/bots/ada", "", true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"group":"工作台"`) {
		t.Fatalf("panel %d %s", code, raw)
	}
	code, raw = call(http.MethodGet, "/api/bots", "", true)
	if !strings.Contains(string(raw), `"group":"工作台"`) {
		t.Fatalf("list %s", raw)
	}

	code, raw = call(http.MethodDelete, "/api/groups/"+url.PathEscape("工作台"), "", true)
	if code != http.StatusOK {
		t.Fatalf("delete %d %s", code, raw)
	}
	if _, err := bot.Load(root, "ada"); err != nil {
		t.Fatal(err)
	}
	ada, err := bot.Load(root, "ada")
	if err != nil || ada.Config().Group != "" {
		t.Fatalf("cleared %v", err)
	}
	code, raw = call(http.MethodGet, "/api/groups", "", true)
	view = decodeFolders(t, raw)
	if view.Folders[len(view.Folders)-1].Count != 2 || len(view.Order) != 0 {
		t.Fatalf("after delete %+v", view)
	}

	code, raw = call(http.MethodPost, "/api/groups", `{"name":"room","members":["ada","bob"]}`, true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"id":"g_`) {
		t.Fatalf("chat group %d %s", code, raw)
	}
	code, raw = call(http.MethodGet, "/api/groups", "", true)
	view = decodeFolders(t, raw)
	if len(view.Groups) != 1 || view.Groups[0].Name != "room" || len(view.Groups[0].Members) != 2 {
		t.Fatalf("chat list %+v %s", view.Groups, raw)
	}
	if len(view.Folders) != 1 || !view.Folders[0].Ungrouped {
		t.Fatalf("folders survived %+v", view.Folders)
	}
}

type folderPayload struct {
	Order   []string `json:"order"`
	Folders []struct {
		Name      string   `json:"name"`
		Count     int      `json:"count"`
		Bots      []string `json:"bots"`
		Ungrouped bool     `json:"ungrouped"`
	} `json:"folders"`
	Groups []struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Members []string `json:"members"`
	} `json:"groups"`
}

func decodeFolders(t *testing.T, raw []byte) folderPayload {
	t.Helper()
	var view folderPayload
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("json %v %s", err, raw)
	}
	return view
}

func mustFolderWebBot(t *testing.T, root, name, owner string) {
	t.Helper()
	b, err := bot.Create(root, name, "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{owner}); err != nil {
		t.Fatal(err)
	}
}
