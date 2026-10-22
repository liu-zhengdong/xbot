//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"xbot/internal/compact"
	"xbot/internal/provider"
)

// M2 fixes admission, queue acknowledgements and context lists. Only the
// explicitly deferred ADR0017 slow-tool latency remains non-fatal.
var baselineGaps = map[string]string{
	"s6/latency": "ADR0017 waits for a running read-only tool; baseline only",
	"s6/visible": "ADR0017 waits for a running read-only tool; baseline only",
}

type busyMeasure struct {
	Scenario       string    `json:"scenario"`
	Task           string    `json:"task"`
	DispatchMS     *float64  `json:"dispatch_ms"`
	FirstVisibleMS *float64  `json:"first_visible_ms"`
	InitialStatus  string    `json:"initial_status"`
	Calls          int       `json:"foreground_calls"`
	Prompt         int       `json:"prompt_tokens"`
	Uncached       int       `json:"uncached_tokens"`
	Completion     int       `json:"completion_tokens"`
	Estimated      bool      `json:"estimated"`
	AUnaffected    bool      `json:"a_unaffected"`
	Reports        int       `json:"reports"`
	Reply          string    `json:"reply"`
	SentAt         time.Time `json:"sent_at"`
	ResultMentions int       `json:"result_mentions"`
	RequestBytes   int       `json:"foreground_request_bytes"`
}

type busyRun struct {
	t             *testing.T
	srv           *Server
	stub          *Stub
	script        *busyScript
	real          bool
	artifactDir   string
	measures      []*busyMeasure
	tasks         map[string][]string
	aIDs          map[string]string
	duplicateSeen map[string]int
}

func (b *busyRun) check(key string, ok bool, detail string) {
	b.t.Helper()
	if ok {
		return
	}
	reason := baselineGaps[key]
	if reason == "" {
		b.t.Errorf("%s: %s", key, detail)
		return
	}
	b.t.Logf("KNOWN_GAP %s: %s; observed: %s", key, reason, detail)
}

// TestNewTaskWhileBusy runs six states through real serve/HTTP/SSE and a
// scripted subagent. XBOT_BUSY_REAL=1 forwards only the foreground; it is
// never enabled by the default smoke/CI commands.
func TestNewTaskWhileBusy(t *testing.T) {
	if os.Getenv("XBOT_BUSY_REAL") == "1" {
		runBusyRealScenarios(t)
		return
	}

	stub := StartStub(t)
	script := &busyScript{stub: stub}
	stub.busy = script
	srv := Start(t, stub)
	b := &busyRun{t: t, srv: srv, stub: stub, script: script, real: script.real != nil, tasks: map[string][]string{}, aIDs: map[string]string{}}
	defer b.summarize()
	// An uninterrupted A is the control for subagent model-call count.
	srv.CreateBot("control", "BOTNAME=control")
	srv.Send("control", busyInput("BUSY_control_A"))
	b.gate("BUSY_control_A")
	stub.Release("BUSY_control_A")
	b.report("control", "BUSY_control_A")

	scenarios := []string{"s1", "s2", "s3", "s4", "s5", "s6"}
	for _, scenario := range scenarios {
		b.scenario(scenario)
	}
	// All scenarios share one serve and one final full scheduling window.
	until := time.Now().Add(servePassInterval + 5*time.Second)
	for time.Now().Before(until) {
		b.checkReports(false)
		time.Sleep(500 * time.Millisecond)
	}
	b.checkReports(true)
}

// Real scenarios have separate serve/stub lifetimes but one whole-run budget.
// A fatal transport/gate error ends only its subtest; metrics still get saved.
func runBusyRealScenarios(t *testing.T) {
	real := newBusyReal(t)
	defer real.finish(t)
	scenarios := []string{"s1", "s2", "s3", "s4", "s5", "s6"}
	if os.Getenv("XBOT_BUSY_S6_ONLY") == "1" {
		scenarios = []string{"s6"}
	}
	for _, scene := range scenarios {
		t.Run(scene, func(t *testing.T) {
			stub := StartStub(t)
			script := &busyScript{stub: stub, real: real}
			stub.busy = script
			srv := Start(t, stub)
			b := &busyRun{t: t, srv: srv, stub: stub, script: script, real: true, tasks: map[string][]string{}, aIDs: map[string]string{}}
			if dir := os.Getenv("XBOT_BUSY_ARTIFACT_DIR"); dir != "" {
				b.artifactDir = filepath.Join(dir, scene)
			}
			defer b.summarize()
			srv.CreateBot("control", "BOTNAME=control")
			srv.Send("control", busyInput("BUSY_control_A"))
			b.gate("BUSY_control_A")
			stub.Release("BUSY_control_A")
			b.report("control", "BUSY_control_A")
			b.scenario(scene)
			until := time.Now().Add(servePassInterval + 5*time.Second)
			for time.Now().Before(until) {
				b.checkReports(false)
				time.Sleep(500 * time.Millisecond)
			}
			b.checkReports(true)
		})
	}
}

func (b *busyRun) gate(name string) {
	WaitFor(b.t, 20*time.Second, "model gate "+name, func() (bool, string) { return b.stub.Entered(name), b.stub.Outcome(name) })
}
func (b *busyRun) report(bot, id string) {
	until := time.Now().Add(20 * time.Second)
	for time.Now().Before(until) {
		if busyReportCount(b.srv.Transcript(bot), id) > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	b.t.Errorf("%s: no assistant report carrying the verification value %s", id, busyCode(id))
}

func (b *busyRun) agent(bot, id string) (Agent, bool) {
	for _, a := range b.srv.Agents(bot) {
		if busyTaskID(a.Task) == id {
			return a, true
		}
	}
	return Agent{}, false
}
func milliseconds(d time.Duration) *float64 { v := float64(d) / float64(time.Millisecond); return &v }

func (b *busyRun) scenario(bot string) {
	aID, a, aStream := b.prepareScenario(bot)
	slow, writerDone := b.slowRead(bot)
	ps := b.sendTasks(bot, a)
	for _, p := range ps {
		r := p.stream.Wait()

		p.m.Reply = r.Done
		if !r.FirstVisible.IsZero() {
			p.m.FirstVisibleMS = milliseconds(r.FirstVisible.Sub(r.Sent))
		}
		suffix := strings.TrimPrefix(p.m.Task, "BUSY_"+bot+"_")
		key := bot + "/dispatch-" + suffix
		if bot == "s6" {
			key = "s6/latency"
		}
		b.check(key, p.m.DispatchMS != nil && (*p.m.DispatchMS <= float64(b.dispatchLimit().Milliseconds())), fmt.Sprintf("record=%s status=%s", busyDelay(p.m.DispatchMS), p.m.InitialStatus))
		if !b.real {
			key := bot + "/visible"
			b.check(key, p.m.FirstVisibleMS != nil && *p.m.FirstVisibleMS <= 3000, busyDelay(p.m.FirstVisibleMS))
		}
		p.m.AUnaffected = p.m.AUnaffected && (bot == "s3" || b.stub.Outcome(aID) == "")
		b.check(bot+"/A", p.m.AUnaffected, "A stopped before explicit release")
	}
	if bot == "s2" {
		r := aStream.Wait()
		b.check(bot+"/handoff", r.HandedOff && b.stub.Outcome("model-s2") == gateCancelled, "old model stream was not cancelled and handed off")
	}
	if slow != nil {
		slow.Wait()
		if err := <-writerDone; err != nil {
			b.t.Fatal(err)
		}
	}
	if bot == "s5" {
		b.check(bot+"/queued", ps[0].m.InitialStatus == "queued", "same-repository B was not queued")
		b.check(bot+"/queue-receipt", busyQueuedReceipt(b.srv.Transcript(bot), ps[0].m.Task), "dispatch did not return queued with a queue position")
	}
	// Sample the context actually presented to the foreground, not code text.
	for _, r := range b.stub.Requests() {
		if r.Bot == bot && r.Caller == callerForeground && busyPhaseFromBody(r.Body) == ps[0].m.Task {
			if bot != "s3" {
				b.check("context-list", busyContextLists(r.Body, a.ID), bot+": no dispatched-agent snapshot in model request")
			}
			break
		}
	}
	for _, p := range ps {
		b.check(bot+"/early-report", busyReportCount(b.srv.Transcript(bot), p.m.Task) == 0, "B reported a result before explicit subagent release")
	}
	b.stub.Release(aID)
	b.report(bot, aID)
	for _, p := range ps {
		if p.m.DispatchMS == nil {
			continue
		}
		b.gate(p.m.Task)
		b.stub.Release(p.m.Task)
		b.report(bot, p.m.Task)
	}
	b.check(bot+"/A-call-count", b.subCalls(aID) == b.subCalls("BUSY_control_A"), fmt.Sprintf("A=%d control=%d", b.subCalls(aID), b.subCalls("BUSY_control_A")))
	b.check(bot+"/A-outcome", b.stub.Outcome(aID) == gateReleased, b.stub.Outcome(aID))
}
func (b *busyRun) dispatchLimit() time.Duration {
	if b.real {
		return 6 * time.Second
	}
	return 3 * time.Second
}
func busyPhaseFromBody(body string) string {
	var r stubRequest
	_ = json.Unmarshal([]byte(body), &r)
	return busyPhase(r)
}
func (b *busyRun) subCalls(id string) int {
	n := 0
	for _, r := range b.stub.Requests() {
		if r.Caller == callerSubagent && busyTaskID(lastUserBody(r.Body)) == id {
			n++
		}
	}
	return n
}
func lastUserBody(body string) string {
	var r stubRequest
	_ = json.Unmarshal([]byte(body), &r)
	return lastUser(r)
}
func (b *busyRun) checkReports(final bool) {
	for bot, ids := range b.tasks {
		rows := b.srv.Transcript(bot)
		for _, id := range ids {
			n := busyReportCount(rows, id)
			if n > 1 && b.duplicateSeen[id] != n {
				if b.duplicateSeen == nil {
					b.duplicateSeen = map[string]int{}
				}
				b.duplicateSeen[id] = n
				b.t.Errorf("%s: %d reports, want 1", id, n)
			}
			if final {
				b.check(bot+"/report-"+strings.TrimPrefix(id, "BUSY_"+bot+"_"), n == 1, fmt.Sprintf("%d reports", n))
				for _, m := range b.measures {
					if m.Task == id {
						m.Reports = n
						m.ResultMentions = 0
						for _, row := range rows {
							if row.Role == "assistant" {
								m.ResultMentions += strings.Count(row.Content, busyCode(id))
							}
						}
					}
				}
			}
		}
	}
}
func (b *busyRun) summarize() {
	var latencies []float64
	for _, m := range b.measures {
		rows := b.srv.Transcript(m.Scenario)
		m.Reports = busyReportCount(rows, m.Task)
		m.ResultMentions = 0
		for _, row := range rows {
			if row.Role == "assistant" {
				m.ResultMentions += strings.Count(row.Content, busyCode(m.Task))
			}
		}
		for _, r := range b.stub.Requests() {
			if r.Caller == callerForeground && busyPhaseFromBody(r.Body) == m.Task {
				m.RequestBytes += len(r.Body)
			}
		}
		if b.real {
			m.Calls, m.Prompt, m.Uncached, m.Completion, m.Estimated = b.script.real.totals(m.Task)
		} else {
			for _, r := range b.stub.Requests() {
				if r.Caller == callerForeground && busyPhaseFromBody(r.Body) == m.Task {
					m.Calls++
					var wire struct {
						Messages []provider.Message `json:"messages"`
					}
					_ = json.Unmarshal([]byte(r.Body), &wire)
					m.Prompt += compact.EstimateMessages(wire.Messages)
					m.Uncached = m.Prompt
				}
			}
		}
		if !b.real {
			b.script.mu.Lock()
			m.Completion = b.script.costs[m.Scenario+"/"+m.Task].completion
			b.script.mu.Unlock()
		}
		if b.real {
			b.t.Logf("BUSY_B_TOKEN_REFERENCE task=%s tokens=%d reference=%d uncached=%d reference=%d (advisory only; never fails or stops forwarding)", m.Task, m.Prompt+m.Completion, busyRealBTokenReference, m.Uncached, busyRealUncachedReference)
		}
		b.check(m.Scenario+"/calls", m.Calls <= 3, fmt.Sprintf("%s: %d foreground calls", m.Task, m.Calls))
		if m.DispatchMS != nil {
			latencies = append(latencies, *m.DispatchMS)
		}
		raw, _ := json.Marshal(m)
		b.t.Logf("BUSY_BASELINE %s", raw)
	}
	sort.Float64s(latencies)
	if len(latencies) > 0 {
		i := (95*len(latencies)+99)/100 - 1
		b.t.Logf("BUSY_P95 dispatched=%d/%d p95_ms=%.3f (missing records are failures, excluded from latency quantile)", len(latencies), len(b.measures), latencies[i])
	}
	for bot, ids := range b.tasks {
		rows := b.srv.Transcript(bot)
		a := ids[0]
		agents := b.srv.Agents(bot)
		records := 0
		for _, ag := range agents {
			if busyTaskID(ag.Task) == a {
				records++
			}
		}
		b.check(bot+"/A-records", records == 1 && busyAStable(agents, b.aIDs[bot], false), fmt.Sprintf("A records=%d, original id=%s", records, b.aIDs[bot]))
		raw, _ := json.Marshal(map[string]any{"scenario": bot, "task": a, "agent_records": records, "model_calls": b.subCalls(a), "control_model_calls": b.subCalls("BUSY_control_A"), "outcome": b.stub.Outcome(a), "report_rows": busyReportCount(rows, a)})
		b.t.Logf("BUSY_A_BASELINE %s", raw)
	}
	if b.real {
		if dir := b.artifactDir; dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				b.t.Error(err)
			} else {
				for bot := range b.tasks {
					agents, _ := json.MarshalIndent(b.srv.Agents(bot), "", "  ")
					if err := os.WriteFile(filepath.Join(dir, bot+"-agents.json"), agents, 0600); err != nil {
						b.t.Error(err)
					}
					raw, _ := json.MarshalIndent(b.srv.Transcript(bot), "", "  ")
					if err := os.WriteFile(filepath.Join(dir, bot+"-transcript.json"), raw, 0600); err != nil {
						b.t.Error(err)
					}
				}
				raw, _ := json.MarshalIndent(b.stub.Requests(), "", "  ")
				if err := os.WriteFile(filepath.Join(dir, "requests.json"), raw, 0600); err != nil {
					b.t.Error(err)
				}
			}
		}
	}
}

// busyQueuedReceipt checks the executor acknowledgement associated with this
// task, rather than requiring one particular Chinese phrase in the reply.
func busyQueuedReceipt(rows []Row, task string) bool {
	calls := map[string]bool{}
	for _, row := range rows {
		for _, tc := range row.ToolCalls {
			if tc.Function.Name != "dispatch" {
				continue
			}
			var args struct {
				Task string `json:"task"`
			}
			if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil && strings.Contains(args.Task, task) {
				calls[tc.ID] = true
			}
		}
		if row.Role == "tool" && calls[row.ToolCallID] && strings.HasPrefix(row.Content, "queued ") && strings.Contains(row.Content, "position ") {
			return true
		}
	}
	return false
}

func (b *busyRun) observationLimit(bot string) time.Duration {
	if b.real {
		return 20 * time.Second
	}
	if bot == "s6" {
		return 15 * time.Second
	}
	return 3500 * time.Millisecond
}
func busyReportCount(rows []Row, id string) int {
	n := 0
	for _, r := range rows {
		if r.Role == "assistant" {
			if strings.Contains(r.Content, busyCode(id)) {
				n++
			}
		}
	}
	return n
}

func busyCapError(rows []Row, id string) bool {
	calls := map[string]bool{}
	for _, r := range rows {
		for _, tc := range r.ToolCalls {
			if tc.Function.Name == "dispatch" && strings.Contains(tc.Function.Arguments, id) {
				calls[tc.ID] = true
			}
		}
		if r.Role == "tool" && calls[r.ToolCallID] && strings.Contains(r.Content, "max concurrent subagents") {
			return true
		}
	}
	return false
}
func busyContextLists(raw, id string) bool {
	var req stubRequest
	_ = json.Unmarshal([]byte(raw), &req)
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		_, rest, ok := strings.Cut(m.Content, "<background_tasks>")
		if ok {
			part, _, _ := strings.Cut(rest, "</background_tasks>")
			return strings.Contains(part, id)
		}
	}
	return false
}

type busyPending struct {
	m        *busyMeasure
	stream   *Stream
	deadline time.Time
}

func (b *busyRun) sendTasks(bot string, a Agent) []busyPending {
	total := 1
	if bot == "s4" {
		total = 5
	}
	var ps []busyPending
	start := time.Now()
	for i := 1; i <= total; i++ {
		if i > 1 {
			time.Sleep(time.Until(start.Add(time.Duration(i-1) * 2 * time.Second)))
		}
		id := fmt.Sprintf("BUSY_%s_B%d", bot, i)
		text := busyInput(id)
		if bot == "s5" {
			text = "在和 A 相同的当前仓库里修改 README，做成另一项开发任务（编号 " + id + "），任务说明保留编号。"
		}
		st := b.srv.BeginSend(bot, text)
		m := &busyMeasure{Scenario: bot, Task: id, AUnaffected: true, Estimated: !b.real, SentAt: st.reply.Sent}
		b.measures = append(b.measures, m)
		b.tasks[bot] = append(b.tasks[bot], id)
		ps = append(ps, busyPending{m, st, st.reply.Sent.Add(b.observationLimit(bot))})
		// Poll all B records at 50 ms while preserving the 2s arrival cadence.
		next := start.Add(time.Duration(i) * 2 * time.Second)
		if i == total {
			next = ps[len(ps)-1].deadline
		}
		for time.Now().Before(next) {
			agents := b.srv.Agents(bot)
			for _, p := range ps {
				if p.m.DispatchMS != nil {
					continue
				}
				for _, ag := range agents {
					if busyTaskID(ag.Task) == p.m.Task && (ag.Status == "running" || ag.Status == "queued") {
						p.m.DispatchMS = milliseconds(time.Since(p.stream.reply.Sent))
						p.m.InitialStatus = ag.Status
						break
					}
				}
			}
			if !busyAStable(agents, a.ID, bot != "s3") {
				for _, p := range ps {
					p.m.AUnaffected = false
				}
			}

			if i == total {
				settled := true
				for _, p := range ps {
					if p.m.DispatchMS == nil {
						select {
						case <-p.stream.done:
						default:
							settled = false
						}
					}
				}
				if settled {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return ps
}

func (b *busyRun) slowRead(bot string) (*Stream, chan error) {
	if bot != "s6" {
		return nil, nil
	}
	var slow *Stream
	var writerDone chan error
	{
		path := filepath.Join(b.srv.Root, "busy-read.fifo")
		if out, err := exec.Command("mkfifo", path).CombinedOutput(); err != nil {
			b.t.Fatalf("mkfifo: %v %s", err, out)
		}
		b.script.slowPath = path
		slow = b.srv.BeginSend(bot, "Use shell to run this plain read command: cat "+shellQuote(path)+". Do not use read_file. BUSY_SLOW_READ")
		WaitFor(b.t, 20*time.Second, "foreground read-only shell running", func() (bool, string) {
			status, raw := b.srv.do("GET", "/api/bots/status", nil)
			return status == 200 && strings.Contains(string(raw), `"tool":"shell"`), string(raw)
		})
		writerDone = make(chan error, 1)
		// Opening the writer unblocks the FIFO reader; withhold bytes and EOF for
		// four seconds after B starts. The real tool, not the model double, waits.
		go func() {
			f, err := os.OpenFile(path, os.O_RDWR, 0)
			if err == nil {
				time.Sleep(4 * time.Second)
				_, err = f.Write([]byte("read complete"))
				f.Close()
			}
			writerDone <- err
		}()
	}
	return slow, writerDone
}

func (b *busyRun) prepareScenario(bot string) (string, Agent, *Stream) {
	b.t.Log("scenario", bot)
	persona := "A and B are separate tasks. Use dispatch, preserve their BUSY identifiers in task descriptions. When each result arrives, report its verification value exactly once. BOTNAME=" + bot
	if bot == "s5" {
		persona += "\nB changes the same repository as A. Dispatch both as dev; say if B is queued."
	}
	b.srv.CreateBot(bot, persona)
	// A real repository fixture avoids a real foreground correctly rejecting
	// the task because the harness supplied an empty directory.
	work := filepath.Join(b.srv.Root, "work", bot)
	if err := os.MkdirAll(work, 0700); err != nil {
		b.t.Fatal(err)
	}
	for name, text := range map[string]string{
		"go.mod": "module busyfixture\n\ngo 1.24\n",
		"sum_test.go": `package busyfixture
import "testing"
func TestSum(t *testing.T){if 1+1!=2 {t.Fatal("sum")}}
`,
	} {
		if err := os.WriteFile(filepath.Join(work, name), []byte(text), 0600); err != nil {
			b.t.Fatal(err)
		}
	}

	aID := "BUSY_" + bot + "_A"
	aStream := b.srv.BeginSend(bot, busyInput(aID))
	b.gate(aID)
	a, ok := b.agent(bot, aID)
	if !ok {
		b.t.Fatalf("%s: A gate reached but no record", bot)
	}
	b.tasks[bot] = []string{aID}
	b.aIDs[bot] = a.ID
	if bot == "s2" {
		b.gate("model-s2")
	} else {
		aStream.Wait()
	}
	if bot == "s3" {
		b.stub.Release(aID)
		// Hold an actual relay model request, not a guessed sleep.
		b.gate("relay-s3")
	}
	return aID, a, aStream
}

func busyDelay(p *float64) string {
	if p == nil {
		return "missing"
	}
	return fmt.Sprintf("%.3fms", *p)
}

func busyAStable(agents []Agent, id string, requireRunning bool) bool {
	for _, a := range agents {
		if a.ID == id {
			return !requireRunning || a.Status == "running"
		}
	}
	return false
}
