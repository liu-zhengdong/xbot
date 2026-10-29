package kernel

import (
	"reflect"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/provider"
	"xbot/internal/skill"
)

// oldPrefix preserves the pre-split layout for the controlled comparison.
func oldPrefix(in Input) []provider.Message {
	text := StablePrefix(in.Persona, in.Rules, in.Overlay, in.Links)
	if in.Installed != "" {
		text += "\n\n" + in.Installed
	}
	if block := skill.FormatPrompt(in.SkillsDir, in.Skills); block != "" {
		text += "\n\n" + block
	}
	return []provider.Message{{Role: provider.RoleSystem, Content: provider.TextContent(text)}}
}

// cachedPrefix models complete-message boundaries, or a finer byte-prefix cache.
// Both stop at the first difference; neither can reuse a later isolated segment.
func cachedPrefix(a, b []provider.Message, whole bool) int {
	n := 0
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i].Role != b[i].Role {
			break
		}
		x, y := a[i].Text(), b[i].Text()
		if x == y {
			n += len(x)
			continue
		}
		if !whole {
			for j := 0; j < len(x) && j < len(y) && x[j] == y[j]; j++ {
				n++
			}
		}
		break
	}
	return n
}

func TestPrefixCacheComparison(t *testing.T) {
	base := Input{Persona: strings.Repeat("persona ", 128), Rules: KernelRules(), Overlay: "model rules", Links: []string{"browser", "telegram"}, Skills: []string{"alpha: first", "beta: second"}, Installed: "Installed connectors: local"}
	cases := []struct {
		name     string
		change   func(*Input)
		retained int
	}{
		{"unchanged", func(*Input) {}, 3},
		{"skill", func(in *Input) { in.Skills = []string{"alpha: changed", "beta: second"} }, 2},
		{"link", func(in *Input) { in.Links = []string{"telegram"} }, 1},
		{"installed", func(in *Input) { in.Installed = "Installed connectors: changed" }, 2},
		{"overlay", func(in *Input) { in.Overlay = "changed model" }, 0},
	}
	before := oldPrefix(base)
	after, _ := Assemble(base)
	t.Logf("prompt estimate before=%d after=%d (includes message overhead)", compact.EstimateMessages(before), compact.EstimateMessages(after))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := base
			tc.change(&changed)
			old := oldPrefix(changed)
			next, _ := Assemble(changed)
			want := 0
			for _, m := range after[:tc.retained] {
				want += len(m.Text())
			}
			if got := cachedPrefix(after, next, true); got != want {
				t.Fatalf("boundary cache %d want %d", got, want)
			}
			for _, whole := range []bool{true, false} {
				a, b := cachedPrefix(before, old, whole), cachedPrefix(after, next, whole)
				// Splitting removes two separator newlines per boundary, not content.
				if b+4 < a {
					t.Fatalf("cache regressed: old=%d new=%d", a, b)
				}
				t.Logf("whole_message=%v cached_bytes before=%d after=%d estimated_tokens before=%d after=%d", whole, a, b, (a+3)/4, (b+3)/4)
			}
		})
	}
	t.Logf("actual segments: S0=%d S1=%q S2=%q", len(after[0].Text()), after[1].Text(), after[2].Text())
}

func TestPrefixSegmentsAndAccounting(t *testing.T) {
	in := Input{Persona: "persona", Rules: KernelRules() + "\n\ncustom supplement", Overlay: "overlay", Links: []string{"browser", "telegram"}, Skills: []string{"alpha: first"}, Installed: "installed", Memory: "</memory> secret", Entries: []bot.Entry{{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("<environment_update>now task state</environment_update>")}}}}
	msgs, usage := Assemble(in)
	if len(msgs) != 5 {
		t.Fatalf("messages=%d", len(msgs))
	}
	for i := 0; i < 3; i++ {
		if msgs[i].Role != provider.RoleSystem {
			t.Fatal("prefix role")
		}
	}
	if msgs[0].Text() != StablePrefix(in.Persona, in.Rules, in.Overlay, nil) || msgs[1].Text() != linksPrefix(in.Links) || !strings.Contains(msgs[2].Text(), skill.FormatPrompt(in.SkillsDir, in.Skills)) {
		t.Fatal("segment contents")
	}
	if !strings.Contains(msgs[3].Text(), "&lt;/memory&gt;") || msgs[4].Text() != in.Entries[0].Message.Text() {
		t.Fatal("memory fencing or history changed")
	}
	stable := 0
	for _, m := range msgs[:3] {
		stable += compact.EstimateMessage(m)
	}
	if usage.Stable != stable || usage.Memory != compact.EstimateMessage(msgs[3]) || usage.Env != compact.EstimateMessage(msgs[4]) {
		t.Fatalf("accounting: %+v", usage)
	}
	again, _ := Assemble(in)
	if !reflect.DeepEqual(msgs, again) || in.Links[0] != "browser" {
		t.Fatal("assembly mutates input or is not repeatable")
	}
	empty, _ := Assemble(Input{})
	if len(empty) != 1 || !strings.Contains(empty[0].Text(), KernelRules()) {
		t.Fatal("empty catalogs or A prime defaults")
	}
}

func BenchmarkPrefixSegments(b *testing.B) {
	in := Input{Persona: strings.Repeat("persona ", 128), Rules: KernelRules(), Links: []string{"terminal", "tools"}, Skills: []string{"alpha: first"}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Assemble(in)
	}
}
