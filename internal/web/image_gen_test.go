package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"xbot/internal/config"
)

func TestImageGenerationSettingsAPI(t *testing.T) {
	s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"configured"},{"id":"runtime-only"}]}`)
	})
	config.SetHeld("XBOT_API_KEY", setupTestKey)
	off := false
	if err := config.Add(s.Root, config.Provider{Name: "vendor", Protocol: config.Responses, BaseURL: config.FromEnv(config.Getenv).BaseURL, APIKeyEnv: "XBOT_API_KEY"}, config.ModelSpec{Name: "configured", Context: 128000, HostedSearch: &off}); err != nil {
		t.Fatal(err)
	}
	path := "/api/providers/models/image-generation"
	body := `{"provider":"vendor","model":"configured","enabled":true,"image_model":" gpt-image-2.5-flare "}`
	req, _ := http.NewRequest("PUT", srv.URL+path, strings.NewReader(body))
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("unauthenticated: %d", res.StatusCode)
	}
	for _, on := range []bool{true, false} {
		body = fmt.Sprintf(`{"provider":"vendor","model":"configured","enabled":%t,"image_model":"gpt-image-2.5-flare"}`, on)
		code, out := setupCall(t, srv, tok, "PUT", path, body)
		if code != 200 || out["hosted_image_generation"] != on || out["image_generation_model"] != "gpt-image-2.5-flare" {
			t.Fatalf("save %d %v", code, out)
		}
		_, spec, err := config.Lookup(s.Root, "vendor", "configured")
		if err != nil || spec.HasHostedImageGen(config.Responses) != on || spec.HasHostedSearch(config.Responses) {
			t.Fatalf("persisted: %+v %v", spec, err)
		}
		code, out = setupCall(t, srv, tok, "GET", "/api/providers/models?provider=vendor", "")
		if code != 200 {
			t.Fatalf("catalog %d %v", code, out)
		}
		models := out["models"].([]any)
		configured := models[0].(map[string]any)
		runtime := models[1].(map[string]any)
		if configured["registered"] != true || configured["hosted_image_generation"] != on || runtime["registered"] != false {
			t.Fatalf("catalog %+v", models)
		}
		code, out = setupCall(t, srv, tok, "GET", "/api/models?all=1", "")
		if code != 200 || out["models"].([]any)[0].(map[string]any)["hosted_image_generation"] != on {
			t.Fatalf("model capabilities %d %v", code, out)
		}
	}
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{`, 400},
		{`{"provider":"vendor","model":"runtime-only","enabled":true}`, 422},
		{`{"provider":"vendor","model":"configured","enabled":true,"image_model":"bad model"}`, 422},
	} {
		code, out := setupCall(t, srv, tok, "PUT", path, tc.body)
		if code != tc.code {
			t.Fatalf("invalid setting %d %v", code, out)
		}
	}
}
