package bot

import (
	"strings"
	"testing"

	"xbot/internal/personacard"
)

const cardHead = "\n\n### 人设卡 · 简洁\n这段只管口吻、节奏和关系分寸，不改上面的职责；两者冲突时职责优先。\n"

// A picked card is a second section after the duty; it never replaces it.
func TestPickedCardStacksOnDuty(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "From persona.md.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	card, err := personacard.Create(root, "u1", "简洁", "先给结论，再给理由。")
	if err != nil {
		t.Fatal(err)
	}
	// A kernel keeps the bot it loaded; the pick happens on another copy, as the web does.
	running, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetPersonaCard(card.ID); err != nil {
		t.Fatal(err)
	}
	got, err := running.ComposedPersona()
	if err != nil || got != "From persona.md."+cardHead+"先给结论，再给理由。" {
		t.Fatalf("picked card not used: %q %v", got, err)
	}
	// The card is referenced, not copied: an edit reaches the bot on its next turn.
	body := "只说一句话。"
	if _, err := personacard.Update(root, "u1", card.ID, nil, &body); err != nil {
		t.Fatal(err)
	}
	if got, _ := running.ComposedPersona(); got != "From persona.md."+cardHead+body {
		t.Fatalf("edited card not used: %q", got)
	}
	// A save from the stale copy must not undo the newer pick.
	desc := "notes"
	if err := running.ApplyProfile(ProfileUpdate{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	fresh, err := Load(root, "ada")
	if err != nil || fresh.Config().PersonaCard != card.ID {
		t.Fatalf("stale save dropped the pick: %+v %v", fresh.Config(), err)
	}
	// Editing the duty while a card is picked reaches the prompt too.
	if err := fresh.WritePersona("New duty."); err != nil {
		t.Fatal(err)
	}
	if got, _ := fresh.ComposedPersona(); got != "New duty."+cardHead+body {
		t.Fatalf("composed %q", got)
	}
	// A card that is gone falls back to the default card; the duty stays.
	if err := personacard.Delete(root, "u1", card.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := fresh.ComposedPersona(); !strings.HasPrefix(got, "New duty.\n\n### 人设卡 · "+personacard.DefaultName+"\n") || !strings.HasSuffix(got, personacard.PartnerPersona) {
		t.Fatalf("missing card %q", got)
	}
	if err := fresh.SetPersonaCard(""); err != nil {
		t.Fatal(err)
	}
	if again, _ := Load(root, "ada"); again.Config().PersonaCard != "" {
		t.Fatal(again.Config())
	}
	if err := fresh.SetPersonaCard("../x"); err == nil {
		t.Fatal("bad id accepted")
	}
}

// A template carries the duty. The card stays in this owner's library and is
// not folded into the duty of the imported bot.
func TestExportTemplateCarriesDutyNotCard(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "legacy persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	card, err := personacard.Create(root, "u1", "简洁", "CURRENT CARD BODY")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetPersonaCard(card.ID); err != nil {
		t.Fatal(err)
	}
	tpl, err := ExportTemplate(b)
	if err != nil || tpl.Persona != "legacy persona" {
		t.Fatalf("exported %q %v", tpl.Persona, err)
	}
	other, err := ImportTemplate(t.TempDir(), tpl, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := other.ComposedPersona(); strings.TrimSpace(got) != "legacy persona" || other.Config().PersonaCard != "" {
		t.Fatalf("imported %q card %q", got, other.Config().PersonaCard)
	}
}
