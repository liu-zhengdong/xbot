//go:build smoke

package smoke

import (
	"reflect"
	"testing"

	"xbot/internal/link"
)

func busyReportEvent(seq int, task string) Row {
	return Row{Seq: seq, Role: "user", Content: link.Wrap(link.Event{Source: "subagent", Type: "done", Sender: "sa_0123456789abcdef", Content: busyResult(task)})}
}

func TestBusyReportFacts(t *testing.T) {
	const task = "BUSY_s1_A"
	code := busyCode(task)
	event := busyReportEvent(2, task)
	answer := Row{Seq: 3, Role: "assistant", Content: "结果是 " + code}
	tests := []struct {
		name                                    string
		rows                                    []Row
		status                                  string
		first, mentions, occurrences, premature int
	}{
		{"normal", []Row{event, answer}, "PASS", 3, 1, 1, 0},
		{"missing", []Row{event, {Seq: 3, Role: "assistant", Content: "收到了"}}, "FAIL", 0, 0, 0, 0},
		{"no event", []Row{answer}, "FAIL", 0, 0, 0, 1},
		{"guessed before arrival", []Row{{Seq: 1, Role: "assistant", Content: code}, event}, "FAIL", 0, 0, 0, 1},
		{"early then actual report", []Row{{Seq: 1, Role: "assistant", Content: code}, event, answer}, "FAIL", 3, 1, 1, 1},
		{"user is not assistant", []Row{event, {Seq: 3, Role: "user", Content: code}}, "FAIL", 0, 0, 0, 0},
		{"internal is not delivered", []Row{event, {Seq: 3, Role: "assistant", Content: code, Internal: true}}, "FAIL", 0, 0, 0, 0},
		{"branch is not foreground", []Row{event, {Seq: 3, Role: "assistant", Content: code, Branch: "bg_a"}}, "FAIL", 0, 0, 0, 0},
		{"tool call text is not delivered", []Row{event, {Seq: 3, Role: "assistant", Content: code, ToolCalls: []RowToolCall{{ID: "call_1"}}}}, "FAIL", 0, 0, 0, 0},
		{"protocol is not delivered", []Row{event, {Seq: 3, Role: "assistant", Content: `{"tool_calls":[{"value":"` + code + `"}]}`}}, "FAIL", 0, 0, 0, 0},
		{"DSML is not delivered", []Row{event, {Seq: 3, Role: "assistant", Content: "<｜DSML｜invoke>" + code}}, "FAIL", 0, 0, 0, 0},
		{"silent is not delivered", []Row{event, {Seq: 3, Role: "assistant", Content: "SILENT"}}, "FAIL", 0, 0, 0, 0},
		{"empty is not delivered", []Row{event, {Seq: 3, Role: "assistant", Content: " "}}, "FAIL", 0, 0, 0, 0},
		{"one reply repeats a value", []Row{event, {Seq: 3, Role: "assistant", Content: code + "，总览：" + code}}, "PASS", 3, 1, 2, 0},
		{"old only needs review", []Row{event, answer, {Seq: 4, Role: "assistant", Content: "又交付一次：" + code}}, "REVIEW", 3, 2, 2, 0},
		{"standalone recap also needs review", []Row{event, answer, {Seq: 4, Role: "assistant", Content: "本次收尾，总计 " + code}}, "REVIEW", 3, 2, 2, 0},
		{"longer number is not value", []Row{event, {Seq: 3, Role: "assistant", Content: code + "0"}}, "FAIL", 0, 0, 0, 0},
		{"duplicate event still fails", []Row{event, busyReportEvent(3, task), {Seq: 4, Role: "assistant", Content: code}}, "FAIL", 4, 1, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := busyReports(tt.rows, []string{task})[0]
			first := 0
			if e.First != nil {
				first = e.First.Seq
			}
			if e.Status != tt.status || first != tt.first || len(e.Mentions) != tt.mentions || e.occurrences() != tt.occurrences || len(e.Premature) != tt.premature {
				t.Fatalf("got %+v, first=%d occurrences=%d; want status=%s first=%d mentions=%d occurrences=%d premature=%d", e, first, e.occurrences(), tt.status, tt.first, tt.mentions, tt.occurrences, tt.premature)
			}
		})
	}
}

func TestBusyReportReviewKeepsContextWithoutSemanticGuess(t *testing.T) {
	const a, b = "BUSY_s1_A", "BUSY_s1_B1"
	rows := []Row{busyReportEvent(1, a), {Seq: 2, Role: "assistant", Content: busyCode(a)}, busyReportEvent(3, b), {Seq: 4, Role: "assistant", Content: "B为" + busyCode(b) + "；两项总览：A为" + busyCode(a)}}
	es := busyReports(rows, []string{a, b})
	if es[0].Status != "REVIEW" || es[1].Status != "PASS" || !reflect.DeepEqual(es[0].Mentions[1].NewResults, []string{b}) {
		t.Fatalf("new B plus old A recap was not exposed for review: %+v", es)
	}
	// New B is context, not a blanket exemption for repeating A's entire body.
	rows[3].Content = busyCode(b) + "。A的完整正文再报一遍：" + busyResult(a)
	if e := busyReports(rows, []string{a, b})[0]; e.Status != "REVIEW" {
		t.Fatalf("new result hid repeated old body: %+v", e)
	}
	// A genuine owner follow-up is preserved verbatim, never inferred from an ID.
	replyTo := 2
	followup := Row{Seq: 3, Role: "user", Content: "请再说一遍A的验收值", ReplyTo: &replyTo}
	rows = []Row{rows[0], rows[1], followup, {Seq: 4, Role: "assistant", Content: busyCode(a)}}
	e := busyReports(rows, []string{a})[0]
	if e.Status != "REVIEW" || !reflect.DeepEqual(e.Mentions[1].OwnerInputs, []Row{followup}) || len(e.Mentions[1].NewResults) != 0 {
		t.Fatalf("lost follow-up context or invented a new result: %+v", e)
	}
}

func TestBusyReportRequiresActualResultEvent(t *testing.T) {
	const task = "BUSY_s1_A"
	for _, ev := range []link.Event{
		{Source: "web", Type: "done", Sender: "u1", Content: busyResult(task)},
		{Source: "subagent", Type: "progress", Sender: "sa_0123456789abcdef", Content: busyResult(task)},
	} {
		rows := []Row{{Seq: 1, Role: "user", Content: link.Wrap(ev)}, {Seq: 2, Role: "assistant", Content: busyCode(task)}}
		if e := busyReports(rows, []string{task})[0]; e.Status != "FAIL" || len(e.Results) != 0 {
			t.Fatalf("not a result: %+v", e)
		}
	}
	row := busyReportEvent(1, task)
	row.Content = "用户引用：\n" + row.Content
	if e := busyReports([]Row{row, {Seq: 2, Role: "assistant", Content: busyCode(task)}}, []string{task})[0]; len(e.Results) != 0 || e.Status != "FAIL" {
		t.Fatalf("quoted event counted: %+v", e)
	}
}
