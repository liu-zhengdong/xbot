//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"xbot/internal/compact"
	"xbot/internal/provider"
)

var busyMarker = regexp.MustCompile(`\bBUSY_(s[1-6]|control)_(A|B[1-5])\b`)

// busyScript is only the model double. It never creates agent records or
// results in the Bot transcript: dispatch, execution and delivery are real.
type busyScript struct {
	stub     *Stub
	real     *busyReal
	slowPath string
	mu       sync.Mutex
	costs    map[string]busyCharge
}

func busyOwners(req stubRequest) []string {
	var out []string
	for _, m := range req.Messages {
		if m.Role != "user" || strings.HasPrefix(m.Content, "<") {
			continue
		}
		out = append(out, busyMarker.FindAllString(m.Content, -1)...)
	}
	return out
}

func busyPhase(req stubRequest) string {
	phase := "other"
	for _, m := range req.Messages {
		if m.Role != "user" || strings.HasPrefix(m.Content, "<environment_") {
			continue
		}
		if strings.HasPrefix(m.Content, "<untrusted_event") {
			phase = "relay"
			continue
		}
		ids := busyMarker.FindAllString(m.Content, -1)
		if len(ids) > 0 {
			phase = ids[len(ids)-1]
		}
	}
	return phase
}

func (b *busyScript) handle(w http.ResponseWriter, r *http.Request, req stubRequest, caller, bot string, raw []byte) bool {
	if caller == callerCompaction || caller == callerExtract {
		return false
	}
	if caller == callerSubagent {
		id := busyTaskID(lastUser(req))
		if id == "" {
			streamText(w, []string{"任务说明缺少 BUSY 编号"})
			return true
		}
		b.stub.Enter(id)
		if b.stub.hold(r.Context(), id) == gateReleased {
			streamText(w, []string{busyResult(id)})
		}
		return true
	}
	if b.real != nil && bot != "control" {
		owners := busyOwners(req)
		hold := ""
		if bot == "s2" && len(owners) == 1 && strings.Contains(joined(req), "started sa_") {
			hold = "model-s2"
		}
		if bot == "s3" && len(owners) == 1 && strings.Contains(lastUser(req), "RESULT_BUSY_s3_A") {
			hold = "relay-s3"
		}
		b.real.forward(w, r, req, raw, hold, b.stub, bot)
		return true
	}
	rw := &busyResponse{ResponseWriter: w}
	w = rw
	defer func() {
		var wire struct {
			Messages []provider.Message `json:"messages"`
		}
		_ = json.Unmarshal(raw, &wire)
		msg := provider.Message{Role: "assistant"}
		for _, line := range strings.Split(rw.data.String(), "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var c struct {
				Choices []struct {
					Delta struct {
						Content string              `json:"content"`
						Tools   []provider.ToolCall `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &c) == nil {
				for _, x := range c.Choices {
					msg.Content += x.Delta.Content
					msg.ToolCalls = append(msg.ToolCalls, x.Delta.Tools...)
				}
			}
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.costs == nil {
			b.costs = map[string]busyCharge{}
		}
		key := bot + "/" + busyPhase(req)
		c := b.costs[key]
		c.prompt += compact.EstimateMessages(wire.Messages)
		c.completion += compact.EstimateMessage(msg)
		b.costs[key] = c
	}()
	time.Sleep(stubFirstDelay)
	owners := busyOwners(req)
	seen := map[string]bool{}
	reported := map[string]bool{}
	for _, m := range req.Messages {
		for _, tc := range m.ToolCalls {
			for _, id := range busyMarker.FindAllString(string(tc), -1) {
				seen[id] = true
			}
		}
		if m.Role == "assistant" && len(m.ToolCalls) == 0 {
			for _, id := range owners {
				if strings.Contains(m.Content, "RESULT_"+id) {
					reported[id] = true
				}
			}
		}
	}
	all := joined(req)
	// Hold actual model requests at the two ADR0017 preemption points.
	if bot == "s2" && len(owners) == 1 && seen[owners[0]] {
		streamHold(w, r.Context(), b.stub, "model-s2", "正在安排，")
		return true
	}
	if bot == "s3" && strings.Contains(all, "RESULT_BUSY_s3_A") && !reported["BUSY_s3_A"] && len(owners) == 1 {
		streamHold(w, r.Context(), b.stub, "relay-s3", "")
		return true
	}
	if bot == "s6" && strings.Contains(all, "BUSY_SLOW_READ") && !busyHasTool(req, "call_busy_read") {
		streamTool(w, "call_busy_read", "shell", argsJSON(map[string]string{"command": "cat " + shellQuote(b.slowPath)}))
		return true
	}
	for _, id := range owners {
		if seen[id] {
			continue
		}
		ty := "research"
		if strings.HasSuffix(id, "_A") || bot == "s5" {
			ty = "dev"
		}
		streamTool(w, "call_"+id, "dispatch", argsJSON(map[string]string{"type": ty, "task": id + "：完成这项任务，结果保留编号；在当前工作目录工作。"}))
		return true
	}
	var reports []string
	for _, id := range owners {
		if !reported[id] && strings.Contains(all, "RESULT_"+id) {
			reports = append(reports, busyResult(id))
		}
	}
	if len(reports) > 0 {
		streamText(w, []string{strings.Join(reports, "；")})
		return true
	}
	// Follow the real tool result: do not invent a queue acknowledgement that
	// dispatch itself did not return. A queued acknowledgement must come from the executor.
	text := "已派出，结果回来后转告。"
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "tool" {
			if strings.HasPrefix(req.Messages[i].Content, "queued ") {
				text = "排队中，有空位后自动开始，结果回来后转告。"
			}
			if strings.Contains(req.Messages[i].Content, "error:") {
				text = req.Messages[i].Content
			}
			break
		}
	}
	streamText(w, []string{text})
	return true
}

func busyInput(id string) string {
	if strings.HasSuffix(id, "_A") {
		return fmt.Sprintf("把当前仓库的测试修好并运行测试（编号 %s）。任务说明保留编号；完成后把验收值转告给我。", id)
	}
	return fmt.Sprintf("另起一项调研：查一下 Go 1.24 改了什么，给出处（编号 %s）。任务说明保留编号，不要改或停止已有任务。", id)
}

// Capture only model output bytes, not request headers or credentials.
// This lets the fake baseline use exactly the kernel's byte estimator for
// completion messages, including tool arguments, instead of reporting zero.
type busyResponse struct {
	http.ResponseWriter
	data strings.Builder
}

func (w *busyResponse) Write(p []byte) (int, error) {
	w.data.Write(p)
	return w.ResponseWriter.Write(p)
}
func (w *busyResponse) Flush() { flush(w.ResponseWriter) }

func busyHasTool(req stubRequest, id string) bool {
	for _, m := range req.Messages {
		for _, tc := range m.ToolCalls {
			if strings.Contains(string(tc), id) {
				return true
			}
		}
	}
	return false
}

func busyCode(id string) string {
	parts := strings.Split(id, "_")
	scenario := 0
	if parts[1] != "control" {
		fmt.Sscanf(parts[1], "s%d", &scenario)
	}
	task := 0
	if parts[2] != "A" {
		fmt.Sscanf(parts[2], "B%d", &task)
	}
	return fmt.Sprintf("731%02d%d", scenario, task)
}
func busyResult(id string) string {
	return "任务检查结束，验收值为 " + busyCode(id) + "。RESULT_" + id
}

// A new B may name A as a constraint. Do not mistake that reference for the
// task's own identity. Multiple different B identifiers are ambiguous.
func busyTaskID(task string) string {
	ids := busyMarker.FindAllString(task, -1)
	unique := map[string]bool{}
	newTasks := map[string]bool{}
	for _, id := range ids {
		unique[id] = true
		if strings.Contains(id, "_B") {
			newTasks[id] = true
		}
	}
	if len(newTasks) == 1 {
		for id := range newTasks {
			return id
		}
	}
	if len(unique) == 1 {
		for id := range unique {
			return id
		}
	}
	return ""
}
