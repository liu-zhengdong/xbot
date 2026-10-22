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
	phase                        string
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
		if err != nil || n < 1 || n > 500000 {
			t.Fatal("XBOT_BUSY_PROMPT_LIMIT must be 1..500000")
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
	prompt, uncached := 0, 0
	for _, c := range b.calls {
		if c.phase == phase {
			prompt += c.prompt
			uncached += c.uncached
		}
	}
	if b.spent()+estimate > b.limit() || (strings.Contains(phase, "_B") && (prompt+estimate > 60000 || uncached+estimate > 15000)) {
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
	b.calls[i] = busyCharge{phase: old.phase, prompt: prompt, uncached: prompt - cached, completion: completion}
	p, u := 0, 0
	for _, c := range b.calls {
		if c.phase == old.phase {
			p += c.prompt
			u += c.uncached
		}
	}
	if b.spent() > b.limit() || (strings.Contains(old.phase, "_B") && (p > 60000 || u > 15000)) {
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
	if b.stopped != "" {
		t.Error(b.stopped)
	}
}
func (b *busyReal) forward(w http.ResponseWriter, r *http.Request, req stubRequest, raw []byte, hold string, stub *Stub) {
	phase := busyPhase(req)
	i, err := b.reserve(phase, (len(raw)+3)/4)
	if err != nil {
		http.Error(w, err.Error(), http.StatusPaymentRequired)
		return
	}
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

func TestBusyRealBudgetStopsBeforeForwarding(t *testing.T) {
	b := &busyReal{}
	if _, e := b.reserve("BUSY_s1_B1", 10000); e != nil {
		t.Fatal(e)
	}
	if _, e := b.reserve("BUSY_s1_B1", 6000); e == nil {
		t.Fatal("uncached budget did not stop forwarding")
	}
	if len(b.calls) != 1 {
		t.Fatal("rejected call was charged/forwarded")
	}
	b = &busyReal{}
	for i := 0; i < 5; i++ {
		if _, e := b.reserve("relay", 100000); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := b.reserve("relay", 1); e == nil {
		t.Fatal("total budget did not stop forwarding")
	}
	b = &busyReal{}
	for i := 0; i < 3; i++ {
		n, e := b.reserve("BUSY_s1_B1", 10000)
		if e != nil {
			t.Fatal(e)
		}
		b.charged(n, 20000, 20000, 1, true)
	}
	if _, e := b.reserve("BUSY_s1_B1", 1); e == nil {
		t.Fatal("per-B prompt budget did not stop forwarding")
	}
}

func (b *busyReal) limit() int {
	if b.maxPrompt > 0 {
		return b.maxPrompt
	}
	return 500000
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
