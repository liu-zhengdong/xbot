package kernel

import (
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func TestStablePrefixSections(t *testing.T) {
	const persona = "### 职责\n这段写你是谁、负责什么、边界在哪；下面是正文，照着做，不要复述本说明。\n"
	const rules = "### 内核规矩\nXBot 硬规矩，与职责和人设卡无关；冲突时本段优先。\n"
	const model = "### 模型补丁\n仅针对当前模型的额外约束。\n"
	const links = "### 能力框\n当前授权的连接/可发现能力；细节按需 load，别假装有未列出的。\n"
	for _, overlay := range []string{"", " \n\t", " model body \n"} {
		want := persona + "persona body\n\n" + rules + "rules body"
		if strings.TrimSpace(overlay) != "" {
			want += "\n\n" + model + "model body"
		}
		if got := StablePrefix(" persona body\n", " rules body\n", overlay, nil); got != want {
			t.Fatalf("sections: got %q want %q", got, want)
		}
		msgs, _ := Assemble(Input{Persona: "persona body", Rules: "rules body", Overlay: overlay, Links: []string{"terminal"}, Skills: []string{"alpha: first"}})
		if len(msgs) != 3 || msgs[0].Text() != want || msgs[1].Text() != links+"- terminal: "+link.Blurb("terminal") || strings.Contains(msgs[2].Text(), "### 能力框") {
			t.Fatalf("S0/S1/S2 boundaries: %+v", msgs)
		}
		if got := StablePrefix("persona body", "rules body", overlay, []string{"terminal"}); got != want+"\n\n"+msgs[1].Text() {
			t.Fatalf("combined prefix order: %q", got)
		}
	}
	if got, want := StablePrefix(" \n", "\t", "", nil), persona+bot.DefaultPersona+"\n\n"+rules+KernelRules(); got != want {
		t.Fatalf("defaults: got %q want %q", got, want)
	}
}
