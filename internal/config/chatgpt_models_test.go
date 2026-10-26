package config

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"xbot/internal/vault"
)

func TestChatGPTModelsCatalogValidation(t *testing.T) {
	for _, mode := range []string{"valid", "unauthorized", "redirect", "malformed", "oversize", "invalid-id", "hidden-only", "too-many"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer scripted-token" {
					t.Error("wrong account catalog request")
				}
				switch mode {
				case "valid":
					fmt.Fprint(w, `{"models":[{"slug":"runtime","visibility":"list","context_window":272000},{"slug":"runtime","visibility":"list"},{"slug":"default","visibility":"list"},{"slug":"hidden","visibility":"hidden"}]}`)
				case "unauthorized":
					http.Error(w, "private-upstream-body", 401)
				case "redirect":
					http.Redirect(w, r, "/must-not-follow", 302)
				case "malformed":
					fmt.Fprint(w, `{"data":[{"id":"wrong-catalog"}],"error":"private-upstream-body"}`)
				case "oversize":
					fmt.Fprint(w, `{"models":[],"padding":"`+strings.Repeat("x", 1<<20)+`"}`)
				case "invalid-id":
					fmt.Fprint(w, `{"models":[{"slug":" ","visibility":"list"}]}`)
				case "hidden-only":
					fmt.Fprint(w, `{"models":[{"slug":"hidden","visibility":"hidden"}]}`)
				case "too-many":
					rows := make([]string, 2049)
					for i := range rows {
						rows[i] = fmt.Sprintf(`{"slug":"model-%d","visibility":"list"}`, i)
					}
					fmt.Fprint(w, `{"models":[`+strings.Join(rows, ",")+`]}`)
				}
			}))
			defer srv.Close()
			backend, err := OAuthVault(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(srv.URL)
			if err := vault.SetOAuth(backend, "chatgpt", vault.OAuthCredential{AccessToken: "scripted-token", RefreshToken: "scripted-refresh", ExpiresAt: time.Now().Add(time.Hour)}, []string{u.Hostname()}); err != nil {
				t.Fatal(err)
			}
			models, err := ChatGPTModels(context.Background(), backend, srv.URL)
			if calls != 1 {
				t.Fatalf("catalog followed redirect: %d", calls)
			}
			if mode == "valid" {
				if err != nil || len(models) != 2 || models[0].Name != "runtime" || models[0].Context != 272000 || models[1].Context != 256000 {
					t.Fatalf("catalog: %v %v", models, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "private-upstream-body") || strings.Contains(err.Error(), "scripted-token") {
				t.Fatalf("unsafe catalog error: %v", err)
			}
		})
	}
}
