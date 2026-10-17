package memory

// Resolve keeps newest-wins across scopes and trust labels (PR #33 round-3 review).

import (
	"testing"
	"time"
)

// A newer trusted person fact must still beat an older trusted bot copy of the
// same attribute key (ADR 0006: facts about the person, newest wins).
func TestM33ResolveNewerPersonAttributeWins(t *testing.T) {
	key := "主人希望白天每两小时被提醒喝水，晚上 21 点后不要再提醒，周末"
	old := Fact{Key: key, Kind: KindAttribute, Scope: ScopeBot, BotName: "health", Text: key + "也一样", Time: time.Unix(100, 0)}
	upd := Fact{Key: key, Kind: KindAttribute, Scope: ScopePerson, PersonID: "u1", Text: key + "不用提醒", Time: time.Unix(200, 0)}
	got := Resolve([]Fact{old, upd})
	if len(got) != 1 || got[0].Text != upd.Text {
		t.Fatalf("stale bot attribute hid the newer person fact: %+v", got)
	}
}

// An owner correction stored as untrusted (its window held a wrapped event)
// must not be hidden forever behind the older trusted fact with the same key.
func TestM33ResolveUntrustedCorrectionNotHidden(t *testing.T) {
	key := "deploy target for the xbot staging environment is the box at the office, reached via"
	old := Fact{Key: key, Kind: KindWork, Scope: ScopeBot, Text: key + " ssh A", Time: time.Unix(100, 0)}
	upd := Fact{Key: key, Kind: KindWork, Scope: ScopeBot, Untrusted: true, Text: key + " ssh B (untrusted, recorded by dev)", Time: time.Unix(200, 0)}
	got := Resolve([]Fact{old, upd})
	if len(got) != 1 || got[0].Text != upd.Text {
		t.Fatalf("newer correction hidden behind older trusted fact: %+v", got)
	}
}
