package config

import "testing"

func TestSeesImagesByModel(t *testing.T) {
	yes := []string{"", "deepseek-flash", "group/auto-deepseek-v4-1-flash", "DeepSeek-Flash"}
	no := []string{"deepseek-chat", "deepseek-v4-pro", "deepseek-reasoner", "gpt-4o", "mock"}
	for _, name := range yes {
		if !SeesImages(name, nil) {
			t.Errorf("%q should see images", name)
		}
	}
	for _, name := range no {
		if SeesImages(name, nil) {
			t.Errorf("%q should not see images", name)
		}
	}
	on, off := true, false
	if !SeesImages("deepseek-v4-pro", &on) {
		t.Fatal("explicit vision did not win")
	}
	if SeesImages("deepseek-flash", &off) {
		t.Fatal("explicit vision off did not win")
	}
}

func TestVisionFlagSurvivesContextUpdate(t *testing.T) {
	root := t.TempDir()
	off := false
	p := Provider{Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_API_KEY"}
	if err := Add(root, p, ModelSpec{Name: "deepseek-flash", Context: 1000, Vision: &off}); err != nil {
		t.Fatal(err)
	}
	if err := Add(root, Provider{Name: "deepseek"}, ModelSpec{Name: "deepseek-flash", Context: 2000}); err != nil {
		t.Fatal(err)
	}
	_, spec, err := Lookup(root, "deepseek", "deepseek-flash")
	if err != nil {
		t.Fatal(err)
	}
	if spec.SeesImages() {
		t.Fatal("context update cleared vision off")
	}
	on := true
	if err := Add(root, Provider{Name: "deepseek"}, ModelSpec{Name: "deepseek-v4-pro", Context: 1000, Vision: &on}); err != nil {
		t.Fatal(err)
	}
	_, pro, err := Lookup(root, "deepseek", "deepseek-v4-pro")
	if err != nil {
		t.Fatal(err)
	}
	if !pro.SeesImages() {
		t.Fatal("explicit on was ignored")
	}
}

func TestLoadWebDefaultsAndRejectsRawKey(t *testing.T) {
	root := t.TempDir()
	f, err := LoadWeb(root)
	if err != nil {
		t.Fatal(err)
	}
	if f.Search.Backend != "" || f.Fetch.Backend != "" {
		t.Fatalf("default file %+v", f)
	}
	if err := SaveWeb(root, WebFile{Search: WebBackend{Backend: "deepseek", APIKeyEnv: "not a name"}}); err == nil {
		t.Fatal("saved a bad env name")
	}
	if err := SaveWeb(root, WebFile{Search: WebBackend{Backend: "deepseek", APIKeyEnv: "SEARCH_KEY"}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_SEARCH_BACKEND", "ddg")
	got, err := LoadWeb(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Search.Backend != "ddg" || got.Search.APIKeyEnv != "SEARCH_KEY" {
		t.Fatalf("env override %+v", got.Search)
	}
	if _, err := SearchKey(WebBackend{Backend: "deepseek", APIKeyEnv: "SEARCH_KEY"}, func(string) string { return "" }); err == nil {
		t.Fatal("empty search key")
	}
}
