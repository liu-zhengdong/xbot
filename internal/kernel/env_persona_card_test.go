package kernel

import (
	"os"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/personacard"
)

// The environment's persona hash follows the persona the prefix uses: the
// picked card. Editing persona.md while a card is picked changes nothing.
func TestEnvironmentPersonaHashFollowsPickedCard(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "From persona.md.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b}
	sample := func() string {
		t.Helper()
		env, err := k.sampleEnvironment(environment{})
		if err != nil {
			t.Fatal(err)
		}
		return environmentGet[string](env, "persona")
	}
	if got := sample(); got != shortHash("From persona.md.") {
		t.Fatalf("no card: %q", got)
	}
	card, err := personacard.Create(root, "u1", "简洁", "先给结论。")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetPersonaCard(card.ID); err != nil {
		t.Fatal(err)
	}
	if got := sample(); got != shortHash("先给结论。") {
		t.Fatalf("picked card: %q", got)
	}
	body := "只说一句。"
	if _, err := personacard.Update(root, "u1", card.ID, nil, &body); err != nil {
		t.Fatal(err)
	}
	if got := sample(); got != shortHash(body) {
		t.Fatalf("edited card: %q", got)
	}
	if err := os.WriteFile(b.PersonaPath(), []byte("unused fallback"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := sample(); got != shortHash(body) {
		t.Fatalf("fallback edit moved the hash: %q", got)
	}
}
