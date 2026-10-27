package kernel

import (
	"os"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/personacard"
	"xbot/internal/provider"
)

// The environment names the duty and the picked card apart, so a card never
// hides a duty edit.
func TestEnvironmentPersonaIdentityNamesDutyAndCard(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "From persona.md.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	running, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: running}
	sample := func() string {
		t.Helper()
		env, err := k.sampleEnvironment(environment{})
		if err != nil {
			t.Fatal(err)
		}
		body, err := running.ComposedPersona()
		if err != nil {
			t.Fatal(err)
		}
		for _, basis := range []string{"", "compressed-range"} {
			text := environmentDelta(environment{}, env, basis, "")
			if strings.Contains(text, body) {
				t.Fatal("persona body leaked into environment")
			}
			if _, ok := decodeEnvironment(text); !ok {
				t.Fatal("persona identity cannot decode")
			}
			replayed := replayEnvironment([]provider.Message{{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(text)}}, "")
			if environmentGet[string](replayed, "persona") != environmentGet[string](env, "persona") {
				t.Fatal("persona identity did not replay")
			}
		}
		return environmentGet[string](env, "persona")
	}
	duty := "职责 " + shortHash("From persona.md.")
	partner := "；人设卡 " + personacard.DefaultName + " " + shortHash(personacard.PartnerPersona)
	if got := sample(); got != duty+partner {
		t.Fatalf("no card: %q", got)
	}
	card, err := personacard.Create(root, "u1", "简洁", "先给结论。")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetPersonaCard(card.ID); err != nil {
		t.Fatal(err)
	}
	if got := sample(); got != duty+"；人设卡 简洁 "+shortHash("先给结论。") {
		t.Fatalf("picked card: %q", got)
	}
	body := "只说一句。"
	if _, err := personacard.Update(root, "u1", card.ID, nil, &body); err != nil {
		t.Fatal(err)
	}
	if got := sample(); got != duty+"；人设卡 简洁 "+shortHash(body) {
		t.Fatalf("edited card: %q", got)
	}
	if err := os.WriteFile(b.PersonaPath(), []byte("new duty"), 0600); err != nil {
		t.Fatal(err)
	}
	duty = "职责 " + shortHash("new duty")
	if got := sample(); got != duty+"；人设卡 简洁 "+shortHash(body) {
		t.Fatalf("duty edit under a card: %q", got)
	}
	name := "简洁新版"
	if _, err := personacard.Update(root, "u1", card.ID, &name, nil); err != nil {
		t.Fatal(err)
	}
	if got := sample(); got != duty+"；人设卡 "+name+" "+shortHash(body) {
		t.Fatalf("renamed card: %q", got)
	}
	if err := personacard.Delete(root, "u1", card.ID); err != nil {
		t.Fatal(err)
	}
	if got := sample(); got != duty+partner {
		t.Fatalf("deleted card: %q", got)
	}
	if err := b.SetPersonaCard(""); err != nil {
		t.Fatal(err)
	}
	if got := sample(); got != duty+partner {
		t.Fatalf("unbound: %q", got)
	}
}

func TestEnvironmentPersonaIdentityReplay(t *testing.T) {
	b, err := bot.Create(t.TempDir(), "ada", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Now: func() time.Time { return time.Unix(0, 0) }}
	prev := environment{}
	// Old hash-only history stays readable and is upgraded by the next diff.
	environmentSet(&prev, "persona", shortHash(bot.DefaultPersona))
	next, err := k.sampleEnvironment(prev)
	if err != nil {
		t.Fatal(err)
	}
	want := "默认职责 " + shortHash(bot.DefaultPersona)
	if got := environmentGet[string](next, "persona"); got != want {
		t.Fatalf("default identity: %q", got)
	}
	for _, basis := range []string{"", "compressed-range"} {
		text := environmentDelta(prev, next, basis, "")
		if strings.Contains(text, bot.DefaultPersona) {
			t.Fatal("environment contains persona body")
		}
		if _, ok := decodeEnvironment(text); !ok {
			t.Fatalf("identity envelope cannot decode: %s", text)
		}
		replayed := replayEnvironment([]provider.Message{{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(text)}}, "")
		if environmentGet[string](replayed, "persona") != want {
			t.Fatal("identity did not replay")
		}
		again, err := k.sampleEnvironment(replayed)
		if err != nil {
			t.Fatal(err)
		}
		if delta := environmentDelta(replayed, again, "", ""); delta != "" {
			t.Fatalf("unchanged identity republished: %s", delta)
		}
	}
}
