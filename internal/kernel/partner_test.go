package kernel

import (
	"encoding/json"
	"strings"
	"testing"

	"xbot/internal/exec"
	"xbot/internal/personacard"
)

func TestPartnerIdentityLivesInThePersona(t *testing.T) {
	if strings.Contains(KernelRules(), "工程合伙人") {
		t.Fatal("partner identity is still a kernel rule")
	}
	if !strings.HasPrefix(KernelRules(), UntrustedRule) {
		t.Fatal("kernel rules do not open with UntrustedRule")
	}
	// The partner identity is the default persona card, stacked after the duty.
	if seed := personacard.Seeds[0]; seed.Name != personacard.DefaultName || seed.Body != personacard.PartnerPersona {
		t.Fatalf("first seed %q is not the partner card", seed.Name)
	}
	b := testBot(t)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if c, ok := b.PickedCard(); !ok || c.Name != personacard.DefaultName {
		t.Fatalf("default card %q %v", c.Name, ok)
	}
	if persona, err := b.ComposedPersona(); err != nil || strings.Count(persona, "你是「工程合伙人」") != 1 {
		t.Fatalf("composed persona %v:\n%s", err, persona)
	}
	// A saved copy of the current defaults is not kept as a supplement.
	if got := botRuleSupplements(DefaultRules() + "\n\nOWNER_SUPPLEMENT"); got != "OWNER_SUPPLEMENT" {
		t.Fatalf("supplements: %q", got)
	}
}

func TestDelegationTreatsSubagentAsColleague(t *testing.T) {
	for _, want := range []string{"能自己判断的同事", "它可以反问", "不要写逐步剧本", "只输出"} {
		if !strings.Contains(DispatchRule, want) {
			t.Fatalf("DispatchRule lacks %q", want)
		}
	}
	for _, want := range []string{"协作伙伴", "步骤由你自己判断", "BLOCKED", "简短的结果摘要", "不会粘贴你的报告"} {
		if !strings.Contains(exec.SubPersona, want) {
			t.Fatalf("SubPersona lacks %q", want)
		}
	}
	raw, err := json.Marshal(dispatchTool{k: &Kernel{Bot: testBot(t)}}.Parameters())
	if err != nil {
		t.Fatal(err)
	}
	var params struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if task := params.Properties["task"].Description; !strings.Contains(task, "意图") || !strings.Contains(task, "验收标准") || !strings.Contains(task, "不要写逐步剧本") {
		t.Fatalf("task description: %q", task)
	}
}
