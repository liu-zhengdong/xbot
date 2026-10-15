package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"xbot/internal/config"
)

func TestModelsAPIHidesUncertified(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Add(root, config.Provider{Name: "fake", BaseURL: "http://127.0.0.1:9/v1", APIKeyEnv: "XBOT_FAKE_UNUSED"}, config.ModelSpec{Name: "scripted", Context: 1024}); err != nil {
		t.Fatal(err)
	}
	if err := config.Add(root, config.Provider{Name: "other", BaseURL: "http://127.0.0.1:9/v1", APIKeyEnv: "XBOT_OTHER_UNUSED"}, config.ModelSpec{Name: "hidden", Context: 8}); err != nil {
		t.Fatal(err)
	}
	if err := config.Certify(root, "fake/scripted"); err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)
	body := getAuth(t, ui.URL+"/api/models", cookie)
	var page struct {
		Models []config.ModelInfo `json:"models"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Models) != 1 || page.Models[0].ID != "fake/scripted" || !page.Models[0].Certified {
		t.Fatalf("default %+v", page.Models)
	}
	all := getAuth(t, ui.URL+"/api/models?all=1", cookie)
	if err := json.Unmarshal(all, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Models) != 2 {
		t.Fatalf("all %+v", page.Models)
	}
	req, _ := http.NewRequest(http.MethodGet, ui.URL+"/api/models?all=2", nil)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("all=2 %d %s", res.StatusCode, raw)
	}
}
