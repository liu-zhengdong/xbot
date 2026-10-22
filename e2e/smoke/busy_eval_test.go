//go:build smoke

package smoke

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"xbot/evals"
	"xbot/internal/config"
	modeleval "xbot/internal/eval"
)

// Manual only: the three new decisions, each three times, without running a
// subagent. The same proxy limits the entire experiment to 270k tokens.
func TestBusyDispatchRealDecisions(t *testing.T) {
	if os.Getenv("XBOT_BUSY_EVAL_REAL") != "1" {
		t.Skip("manual real-model decision baseline; default off")
	}
	real := newBusyReal(t)
	if real.limit() > 270000 {
		real.maxPrompt = 270000
	}
	defer real.finish(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "request read", 400)
			return
		}
		var req stubRequest
		if json.Unmarshal(raw, &req) != nil {
			http.Error(w, "request json", 400)
			return
		}
		real.forward(w, r, req, raw, "", nil)
	}))
	defer proxy.Close()
	root := t.TempDir()
	cases := t.TempDir()
	for _, name := range []string{"busy-new-task", "busy-steer", "busy-same-repo"} {
		raw, err := evals.Files.ReadFile("dispatch/" + name + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(cases, name+".yaml"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("BUSY_EVAL_PROXY_KEY", "local-proxy-only")
	if err := config.Add(root, config.Provider{Name: "deepseek", BaseURL: proxy.URL + "/v1", APIKeyEnv: "BUSY_EVAL_PROXY_KEY"}, config.ModelSpec{Name: real.model, Context: 1048576, Thinking: "off"}); err != nil {
		t.Fatal(err)
	}
	out := os.Getenv("XBOT_BUSY_REPORT_DIR")
	if out == "" {
		out = t.TempDir()
	}
	report, err := modeleval.Run(modeleval.Options{Root: root, Model: "deepseek/" + real.model, Suite: "dispatch-busy", CasesDir: cases, OutDir: out, Runs: 3})
	for _, c := range report.Cases {
		n := 0
		for _, r := range c.Runs {
			if r.Pass {
				n++
			}
		}
		t.Logf("BUSY_DECISION case=%s pass=%d/%d pass^3=%t reason=%s", c.Name, n, len(c.Runs), n == 3, c.Reason)
	}
	if err != nil {
		t.Errorf("real decision baseline: %v (report %s)", err, report.JSON)
	}
}
