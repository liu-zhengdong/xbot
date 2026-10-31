//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/link"
)

// Real serve/config/API/SSE path; only the two upstream models are scripted.
func TestRateLimitBinary(t *testing.T) {
	for _, backup := range []bool{false, true} {
		t.Run(fmt.Sprint(backup), func(t *testing.T) {
			var mainCalls, backupCalls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model string `json:"model"`
					stubRequest
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if req.Model == "flash" {
					backupCalls.Add(1)
					if !sawTool(req.stubRequest, "send_message_to_user") {
						streamTool(w, "notify", "send_message_to_user", `{"text":"backup finished"}`)
					} else {
						streamText(w, []string{"SILENT"})
					}
					return
				}
				mainCalls.Add(1)
				w.Header().Set("Retry-After", "7200")
				w.WriteHeader(429)
				fmt.Fprint(w, `{"error":{"code":"usage_limit_reached"}}`)
			}))
			defer up.Close()
			srv := Start(t, StartStub(t))
			srv.CreateBot("quota", "Reply briefly without tools.")
			srv.Stop()
			f, err := config.Load(srv.Root)
			if err != nil {
				t.Fatal(err)
			}
			for i := range f.Providers {
				if f.Providers[i].Name == "deepseek" {
					f.Providers[i].BaseURL = up.URL
					f.Providers[i].RateLimitWaitSeconds = 1
					if backup {
						f.Providers[i].Fallback = &config.FallbackBinding{Provider: "backup", Model: "flash"}
					}
				}
			}
			f.Providers = append(f.Providers, config.Provider{Name: "backup", BaseURL: up.URL, APIKeyEnv: "XBOT_API_KEY", Models: []config.ModelSpec{{Name: "flash"}}})
			if err := config.Save(srv.Root, f); err != nil {
				t.Fatal(err)
			}
			srv.Restart()
			started := time.Now()
			stream := srv.BeginSend("quota", "Please reply.")
			<-stream.done
			elapsed := time.Since(started)
			if stream.failure != "" {
				t.Fatal(stream.failure)
			}
			if backup {
				if stream.reply.Error != "" || stream.reply.Done != "backup finished" || backupCalls.Load() != 2 {
					t.Fatalf("backup result: %+v, calls=%d", stream.reply, backupCalls.Load())
				}
			} else if !strings.Contains(stream.reply.Error, "限流等待过久") || backupCalls.Load() != 0 {
				t.Fatalf("terminal result: %+v, backup calls=%d", stream.reply, backupCalls.Load())
			}
			if mainCalls.Load() != 1 || elapsed > 3*time.Second {
				t.Fatalf("calls=%d elapsed=%s", mainCalls.Load(), elapsed)
			}
			t.Logf("real binary quota response: %s; primary=%d backup=%d", elapsed, mainCalls.Load(), backupCalls.Load())
			if !backup {
				// Also exercise unattended timer delivery. The existing trace records
				// failed status; the existing serve error sink records the cause.
				srv.Stop()
				b, err := bot.Load(srv.Root, "quota")
				if err != nil {
					t.Fatal(err)
				}
				timer := &link.Timer{Dir: b.RoutineDir()}
				if err := timer.Save(link.Routine{ID: "quota-night", Prompt: "Reply briefly.", At: time.Now().Add(-time.Second).Format(time.RFC3339), Zone: "local", Enabled: true}); err != nil {
					t.Fatal(err)
				}
				started := time.Now()
				srv.Restart()
				WaitFor(t, 5*time.Second, "timer quota failure", func() (bool, string) {
					traces, err := b.Traces()
					failed := 0
					for _, trace := range traces {
						if trace.Status == "failed" {
							failed++
						}
					}
					return err == nil && failed >= 2 && mainCalls.Load() == 2 && strings.Contains(srv.Stderr(), "限流等待过久"), srv.Stderr()
				})
				routine, err := timer.Load("quota-night")
				if err != nil || routine.LastSlot == "" || backupCalls.Load() != 0 {
					t.Fatalf("timer not consumed cleanly: %+v %v", routine, err)
				}
				t.Logf("unattended timer failed cleanly in %s; slot=%s", time.Since(started), routine.LastSlot)
			}
		})
	}
}
