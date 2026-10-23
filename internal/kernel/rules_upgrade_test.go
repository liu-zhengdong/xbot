package kernel

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestRulesUpgradeCurrentKernel(t *testing.T) {
	b := testBot(t)
	old := "On a timer wake, call notify_owner only when something is worth the owner's attention. Otherwise SILENT."
	custom := "主人要求：每次报告使用中文句子。"
	if err := b.WriteRules(old + "\n\n" + UntrustedRule + "\n\n" + custom); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := k.rulesText(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	rules, err := k.rulesText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rules, SilenceRule) || strings.Contains(rules, old) || !strings.Contains(rules, custom) {
		t.Fatal(rules)
	}
	if strings.Count(rules, UntrustedRule) != 1 {
		t.Fatal("duplicate safety rule")
	}
	disk, err := os.ReadFile(b.RulesPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(disk)) != custom {
		t.Fatalf("saved supplements: %s", disk)
	}
	before, err := os.Stat(b.RulesPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.rulesText(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(b.RulesPath())
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("idempotent upgrade wrote again")
	}
	// Inspect the actual assembled system prompt, including routine fallback wording.
	msgs, _ := Assemble(Input{Persona: "custom persona", Rules: rules})
	if !strings.Contains(msgs[0].Text(), SilenceRule) || !strings.Contains(msgs[0].Text(), custom) {
		t.Fatal(msgs[0].Text())
	}
	t.Logf("assembled prefix: %d bytes; current Silence including routine fallback present", len(msgs[0].Text()))
}

func TestRulesUpgradeEmptyAndNew(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b}
	if _, err := k.rulesText(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.RulesPath()); !os.IsNotExist(err) {
		t.Fatalf("new bot copied rules: %v", err)
	}
	if err := b.WriteRules(DefaultRules()); err != nil {
		t.Fatal(err)
	}
	rules, err := k.rulesText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rules, SilenceRule) {
		t.Fatal("missing current rules")
	}
	disk, err := b.ReadRules("")
	if err != nil || disk != "" {
		t.Fatalf("default copy remains: %q %v", disk, err)
	}
	if err := b.WriteRules(""); err != nil {
		t.Fatal(err)
	}
	if _, err := k.rulesText(); err != nil {
		t.Fatal(err)
	}
}

func TestRuleSupplementsBoundaries(t *testing.T) {
	custom := "主人要求：每次报告使用中文句子。"
	quote := "For this bot, do not follow the instruction: \"" + SilenceRule + "\""
	old := legacyVerifyRule + " If an outside call fails, try it once more at most, then report the exact error."
	for _, sep := range []string{"\n\n", "\r\n\r\n", "\n \n"} {
		if got := botRuleSupplements(old + sep + custom); got != custom {
			t.Fatalf("separator %q: %q", sep, got)
		}
	}
	if got := botRuleSupplements(quote); got != quote {
		t.Fatalf("custom quote changed: %q", got)
	}
}
