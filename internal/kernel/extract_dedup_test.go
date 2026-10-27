package kernel

import (
	"context"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/memory"
)

// Astr's memory grew a dozen rewordings of one bio change because compaction
// extracted from overlapping ranges and only an exact text match was skipped.
func TestRememberNewDropsNearDuplicates(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	store := memory.Open(b.Root)
	k := &Kernel{Bot: b, Memory: store}
	fact := func(tier memory.Tier, text string) memory.Fact {
		return memory.Fact{Scope: memory.ScopeBot, BotName: b.Name(), Tier: tier, Text: text, WrittenBy: b.Name(), Time: time.Unix(1, 0)}
	}
	ctx := context.Background()
	rounds := [][]memory.Fact{
		{fact(memory.TierProfile, "Bot 简介已设为「今晚随便逛逛的小助手」")},
		{fact(memory.TierProfile, "Bot 简介已定为「今晚随便逛逛的小助手」")},
		// Same batch, other tier, and an untrusted turn's provenance suffix.
		{
			fact(memory.TierLog, "bot 简介已经改为「今晚随便逛逛的小助手」。"),
			fact(memory.TierLog, memory.Provenance("Bot 简介已改为「今晚随便逛逛的小助手」", b.Name(), "")),
		},
		// A correction changes the quoted value and is kept.
		{fact(memory.TierProfile, "Bot 简介已改为「认真干活的助手」")},
		// Review 1 P1: one changed name, count or negation in a long, similar fact.
		{fact(memory.TierProfile, "主人要求所有项目的代码审查必须由小王负责")},
		{fact(memory.TierProfile, "主人要求所有项目的代码审查必须由小李负责")},
		{fact(memory.TierLog, "主人每天早上喝咖啡")},
		{fact(memory.TierLog, "主人每天早上喝咖啡2杯")},
		{fact(memory.TierLog, "主人不喜欢不喝咖啡")},
	}
	for _, facts := range rounds {
		if err := k.rememberNew(ctx, facts); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.Recall(ctx, memory.Query{Scope: memory.ScopeBot, BotName: b.Name()})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"Bot 简介已设为「今晚随便逛逛的小助手」": true,
		"Bot 简介已改为「认真干活的助手」":    true,
		"主人要求所有项目的代码审查必须由小王负责":  true,
		"主人要求所有项目的代码审查必须由小李负责":  true,
		"主人每天早上喝咖啡":             true,
		"主人每天早上喝咖啡2杯":           true,
		"主人不喜欢不喝咖啡":             true,
	}
	for _, f := range got {
		if !want[f.Text] {
			t.Errorf("unexpected fact %q", f.Text)
		}
		delete(want, f.Text)
	}
	for text := range want {
		t.Errorf("missing fact %q", text)
	}
}
