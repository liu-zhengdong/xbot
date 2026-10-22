//go:build smoke

package smoke

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real credentials come only from the invoking process's environment. No
// config files or owner data are read by the test, and keys are never logged.
// Set XBOT_BUSY_REAL=1, XBOT_BUSY_BASE_URL, XBOT_BUSY_API_KEY and optionally
// XBOT_BUSY_MODEL (default deepseek-flash). Only foreground requests leave
// the stub. Subagents/compaction/extraction always stay scripted.
const (
	busyRealTokenLimit        = 100_000_000
	busyRealBTokenReference   = 10_000_000
	busyRealUncachedReference = 500_000
)

type busyReal struct {
	mu               sync.Mutex
	base, key, model string
	client           *http.Client
	calls            []busyCharge
	stopped          string
	total            int
	maxPrompt        int
}
type busyCharge struct {
	phase, scenario              string
	prompt, uncached, completion int
	estimated                    bool
}

func newBusyReal(t *testing.T) *busyReal {
	t.Helper()
	b := &busyReal{base: strings.TrimRight(os.Getenv("XBOT_BUSY_BASE_URL"), "/"), key: os.Getenv("XBOT_BUSY_API_KEY"), model: os.Getenv("XBOT_BUSY_MODEL"), client: &http.Client{Timeout: 45 * time.Second}}
	if b.model == "" {
		b.model = "deepseek-flash"
	}
	if v := os.Getenv("XBOT_BUSY_PROMPT_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > busyRealTokenLimit {
			t.Fatal("XBOT_BUSY_PROMPT_LIMIT must be 1..100000000")
		}
		b.maxPrompt = n
	}
	if b.base == "" || b.key == "" {
		t.Fatal("XBOT_BUSY_REAL requires XBOT_BUSY_BASE_URL and XBOT_BUSY_API_KEY; values are never logged")
	}
	return b
}
func (b *busyReal) reserve(phase string, estimate int) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped != "" {
		return -1, fmt.Errorf("%s", b.stopped)
	}
	// Per-B thresholds are measurements, not forwarding limits.
	if b.spent()+estimate > b.limit() {
		b.stopped = "real foreground budget would be exceeded; forwarding stopped"
		return -1, fmt.Errorf("%s", b.stopped)
	}
	i := len(b.calls)
	b.calls = append(b.calls, busyCharge{phase: phase, prompt: estimate, uncached: estimate, estimated: true})
	b.total += estimate
	return i, nil
}
func (b *busyReal) charged(i, prompt, cached, completion int, reported bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if prompt < 0 || cached < 0 || cached > prompt || completion < 0 {
		b.stopped = "invalid model-reported usage; forwarding stopped"
		return
	}
	if !reported {
		return
	} // cancelled/unreported calls keep the conservative reservation
	old := b.calls[i]
	b.total += prompt - old.prompt
	b.calls[i] = busyCharge{phase: old.phase, scenario: old.scenario, prompt: prompt, uncached: prompt - cached, completion: completion}
	if b.spent() > b.limit() {
		b.stopped = "model-reported usage crossed budget; no more calls will be forwarded"
	}
}
func (b *busyReal) totals(phase string) (calls, prompt, uncached, completion int, estimated bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.calls {
		if c.phase == phase {
			calls++
			prompt += c.prompt
			uncached += c.uncached
			completion += c.completion
			estimated = estimated || c.estimated
		}
	}
	return
}
func (b *busyReal) finish(t *testing.T) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var uncached, completion, reportedPrompt, reservedPrompt int
	for _, c := range b.calls {
		uncached += c.uncached
		completion += c.completion
		if c.estimated {
			reservedPrompt += c.prompt
		} else {
			reportedPrompt += c.prompt
		}
	}
	t.Logf("BUSY_REAL_ACCOUNTING reported_prompt=%d conservative_unreported_prompt=%d total_tokens=%d", reportedPrompt, reservedPrompt, b.spent())
	t.Logf("BUSY_REAL_TOTAL calls=%d prompt=%d uncached=%d completion=%d limit=%d", len(b.calls), b.total, uncached, completion, b.limit())
	if dir := os.Getenv("XBOT_BUSY_ARTIFACT_DIR"); dir != "" {
		accounts := map[string][]map[string]any{}
		for _, c := range b.calls {
			accounts[c.scenario] = append(accounts[c.scenario], map[string]any{"phase": c.phase, "prompt": c.prompt, "uncached": c.uncached, "completion": c.completion, "estimated": c.estimated})
		}
		raw, _ := json.MarshalIndent(accounts, "", "  ")
		name := strings.ReplaceAll(t.Name(), "/", "_") + "-accounting.json"
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Error(err)
		} else if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Error(err)
		}
	}
	if b.stopped != "" {
		t.Error(b.stopped)
	}
}
func (b *busyReal) forward(w http.ResponseWriter, r *http.Request, req stubRequest, raw []byte, hold string, stub *Stub, scenario string) {
	phase := busyPhase(req)
	i, err := b.reserve(phase, (len(raw)+3)/4)
	if err != nil {
		http.Error(w, err.Error(), http.StatusPaymentRequired)
		return
	}
	b.mu.Lock()
	b.calls[i].scenario = scenario
	b.mu.Unlock()
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	body["model"] = b.model
	body["thinking"] = map[string]string{"type": "disabled"}
	payload, _ := json.Marshal(body)
	out, err := http.NewRequestWithContext(r.Context(), "POST", b.base+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		http.Error(w, "invalid real endpoint", 400)
		return
	}
	out.Header.Set("Authorization", "Bearer "+b.key)
	out.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(out)
	if err != nil {
		http.Error(w, "real foreground transport failed", 502)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		http.Error(w, fmt.Sprintf("real foreground status %d", resp.StatusCode), 502)
		return
	}
	if hold != "" {
		streamHold(w, r.Context(), stub, hold, "")
		return
	}
	startSSE(w)
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 4<<20))
	sc.Buffer(make([]byte, 65536), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") && strings.TrimSpace(strings.TrimPrefix(line, "data: ")) != "[DONE]" {
			var chunk struct {
				Usage *struct {
					Prompt     *int `json:"prompt_tokens"`
					Completion int  `json:"completion_tokens"`
					Hit        *int `json:"prompt_cache_hit_tokens"`
					Details    *struct {
						Cached int `json:"cached_tokens"`
					} `json:"prompt_tokens_details"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk) == nil && chunk.Usage != nil && chunk.Usage.Prompt != nil {
				u := chunk.Usage
				cached := 0
				if u.Details != nil {
					cached = u.Details.Cached
				} else if u.Hit != nil {
					cached = *u.Hit
				}
				b.charged(i, *u.Prompt, cached, u.Completion, true)
			}
		}
		fmt.Fprintln(w, line)
		flush(w)
	}
}

// Per-B overruns must not prevent measuring subsequent calls or scenarios.
func TestBusyRealBudgetStopsBeforeForwarding(t *testing.T) {
	b := &busyReal{}
	if b.limit() != 100_000_000 {
		t.Fatal("default whole-run cap is not 100M")
	}
	for i := 0; i < 4; i++ {
		n, err := b.reserve("BUSY_s1_B1", 3_000_000)
		if err != nil {
			t.Fatal("per-B metric stopped forwarding:", err)
		}
		b.charged(n, 3_000_000, 0, 10, true)
	}
	_, prompt, uncached, _, _ := b.totals("BUSY_s1_B1")
	if prompt != 12_000_000 || uncached != 12_000_000 {
		t.Fatal("overruns were not measured")
	}
	if _, err := b.reserve("BUSY_s2_B1", 1); err != nil {
		t.Fatal("one scenario blocked the next:", err)
	}
	b = &busyReal{}
	if _, err := b.reserve("relay", 100_000_000); err != nil {
		t.Fatal(err)
	}
	if _, err := b.reserve("relay", 1); err == nil || len(b.calls) != 1 {
		t.Fatal("whole-run cap did not stop before forwarding")
	}
	b = &busyReal{maxPrompt: 100}
	n, _ := b.reserve("relay", 90)
	b.charged(n, 90, 0, 11, true)
	if _, err := b.reserve("BUSY_s2_B1", 1); err == nil {
		t.Fatal("completion was excluded from whole-run cap")
	}
}

func TestBusyRealAccountingSeparatesExperiments(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XBOT_BUSY_ARTIFACT_DIR", dir)
	for _, phase := range []string{"foreground", "dispatch-eval"} {
		t.Run(phase, func(t *testing.T) {
			b := &busyReal{}
			if _, err := b.reserve(phase, 1); err != nil {
				t.Fatal(err)
			}
			b.finish(t)
		})
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d accounting files, want 2 independent experiments", len(entries))
	}
	phases := map[string]bool{}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var ledger map[string][]map[string]any
		if err := json.Unmarshal(raw, &ledger); err != nil {
			t.Fatal(err)
		}
		for _, calls := range ledger {
			for _, call := range calls {
				phases[call["phase"].(string)] = true
			}
		}
	}
	if !phases["foreground"] || !phases["dispatch-eval"] {
		t.Fatal("an experiment's accounting was overwritten")
	}
}

func (b *busyReal) limit() int {
	if b.maxPrompt > 0 {
		return b.maxPrompt
	}
	return busyRealTokenLimit
}

func TestBusyReportCountsAssistantRowsNotRepeatedWords(t *testing.T) {
	id := "BUSY_s1_A"
	code := busyCode(id)
	rows := []Row{{Role: "user", Content: code}, {Role: "assistant", Content: code + " is the value; result " + code}}
	if n := busyReportCount(rows, id); n != 1 {
		t.Fatalf("one report row counted %d", n)
	}
	rows = append(rows, Row{Role: "assistant", Content: "again " + code})
	if n := busyReportCount(rows, id); n != 2 {
		t.Fatalf("second report row counted %d", n)
	}
}

// Called while mu is held. Whole-run budgeting includes output tokens too.
func (b *busyReal) spent() int {
	n := b.total
	for _, c := range b.calls {
		n += c.completion
	}
	return n
}

func TestBusyTaskIdentityDoesNotCountAReferenceAsAnotherA(t *testing.T) {
	task := "BUSY_s1_B1: research Go; leave BUSY_s1_A running"
	if got := busyTaskID(task); got != "BUSY_s1_B1" {
		t.Fatalf("task id %q", got)
	}
	if got := busyTaskID("BUSY_s1_B1 or BUSY_s1_B2"); got != "" {
		t.Fatalf("ambiguous task accepted as %q", got)
	}
}

func TestBusyAObservationRejectsDisappearanceAndCancellation(t *testing.T) {
	a := Agent{ID: "sa_0123456789abcdef", Status: "running"}
	if !busyAStable([]Agent{a}, a.ID, true) {
		t.Fatal("running A rejected")
	}
	if busyAStable(nil, a.ID, true) || busyAStable([]Agent{{ID: "sa_fedcba9876543210", Status: "running"}}, a.ID, true) {
		t.Fatal("missing/replaced A accepted")
	}
	a.Status = "cancelled"
	if busyAStable([]Agent{a}, a.ID, true) {
		t.Fatal("cancelled A accepted")
	}
	a.Status = "done"
	if !busyAStable([]Agent{a}, a.ID, false) {
		t.Fatal("relay scenario must allow completed A")
	}
}
