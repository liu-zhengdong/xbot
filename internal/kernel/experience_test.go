package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/experience"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestExperienceRecordShowsInNextEnvironment(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory, link.NameContext)
	p := &scripted{resps: []provider.Message{
		toolCall("c1", "experience_record", `{"summary":"决定部署改成蓝绿发布","focus":"deploy"}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("记下了")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("在")},
	}}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.Open(b.Root), MaxSteps: 4, Links: []link.Link{link.Memory{}, link.Context{}}}
	ctx := context.Background()
	if err := k.Handle(ctx, link.TerminalEvent("u1", "部署以后走蓝绿", time.Now())); err != nil {
		t.Fatal(err)
	}
	first := p.reqs[0]
	if !strings.Contains(messagesText(first.Messages), ExperienceRule) {
		t.Fatal("experience rule missing from the request")
	}
	var named bool
	for _, tool := range first.Tools {
		named = named || tool.Name == "experience_record"
	}
	if !named {
		t.Fatal("experience_record not offered")
	}
	if got := messagesText(p.reqs[1].Messages); !strings.Contains(got, "recorded experience #1 (seq ") {
		t.Fatalf("tool result: %s", got)
	}
	e, n, ok, err := experience.At(bot.Dir(b.Root, b.Name())).Latest()
	if err != nil || !ok || n != 1 {
		t.Fatal(n, ok, err)
	}
	owner, err := b.Slice(e.From, e.From)
	if err != nil {
		t.Fatal(err)
	}
	if e.Summary != "决定部署改成蓝绿发布" || e.Focus != "deploy" || e.To < e.From || !strings.Contains(owner[0].Message.Text(), "部署以后走蓝绿") || !strings.HasPrefix(e.Segment, "user: 部署以后走蓝绿\n") || !strings.Contains(e.Segment, "experience_record") {
		t.Fatalf("stored %+v / %q", e, owner[0].Message.Text())
	}
	if err := k.Handle(ctx, link.TerminalEvent("u1", "在吗", time.Now())); err != nil {
		t.Fatal(err)
	}
	last := p.reqs[len(p.reqs)-1].Messages
	var env string
	for _, m := range last {
		if m.Internal && strings.Contains(m.Text(), `"experience"`) {
			env = m.Text()
		}
	}
	if !strings.Contains(env, `"count":1`) || !strings.Contains(env, "#1 决定部署改成蓝绿发布") {
		t.Fatalf("next environment lacks the new experience: %s", messagesText(last))
	}
}

func TestExperienceSearchLeavesOutToolBodyAndDetailReadsIt(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory, link.NameContext)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}}
	body := "TOOLBODY_MARK " + strings.Repeat("x", 6000) + " TOOLBODY_END"
	for _, e := range []bot.Entry{
		{Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("看看日志为什么报错")}},
		{Message: toolCall("c1", "shell", `{"command":"tail app.log"}`)},
		{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("c1", body)}},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("是磁盘满了")}},
	} {
		e.Time = time.Now()
		if err := b.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	rec, err := k.RecordExperience("排查日志报错，原因是磁盘满了", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rec.From != 1 || rec.To != 4 {
		t.Fatalf("anchor %d-%d", rec.From, rec.To)
	}
	for _, q := range []string{"磁盘", ""} {
		got, err := k.SearchExperiences(q, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "#1 ") || !strings.Contains(got, "原因是磁盘满了") || !strings.Contains(got, "[tools: shell]") || strings.Contains(got, "TOOLBODY") {
			t.Fatalf("search %q: %s", q, got)
		}
	}
	if got, _ := k.SearchExperiences("火星", 0); got != "no matching experiences" {
		t.Fatal(got)
	}
	for _, args := range [][2]int{{3, 0}, {2, 1}} {
		got, err := k.Detail(args[0], int64(args[1]))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, "# seq 3 tool") || !strings.Contains(got, "TOOLBODY_END") {
			t.Fatalf("detail %v: %.200s", args, got)
		}
	}
	if _, err := k.Detail(9, 1); err == nil {
		t.Fatal("index past the experience range accepted")
	}
	if _, err := k.RecordExperience("x", "", 3, 9); err == nil {
		t.Fatal("range past the transcript accepted")
	}
}
