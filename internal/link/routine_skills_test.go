package link

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRoutineSkillsEvents(t *testing.T) {
	now := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	for _, once := range []bool{false, true} {
		tm := &Timer{Dir: t.TempDir()}
		r := Routine{ID: "check", Name: "Check", Prompt: "inspect", Enabled: true, Skills: []string{"atlas", "ship"}}
		if once {
			r.At = now.Format(time.RFC3339)
		} else {
			r.Cron = "0 * * * *"
		}
		if err := tm.Save(r); err != nil {
			t.Fatal(err)
		}
		events, err := tm.Due(now)
		if err != nil || len(events) != 1 {
			t.Fatalf("Due %v %v", events, err)
		}
		ev := events[0]
		if ev.Content != RoutineBody(r.ID, r.Name, r.Prompt) || strings.Join(ev.RoutineSkills, ",") != "atlas,ship" {
			t.Fatalf("event %+v", ev)
		}
		// The event's JSON is the inbox contract; it must carry references only.
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		var restored Event
		if err := json.Unmarshal(raw, &restored); err != nil || strings.Join(restored.RoutineSkills, ",") != "atlas,ship" {
			t.Fatalf("restored %+v %v", restored, err)
		}
		r.Skills = nil
		if err := tm.Save(r); err != nil {
			t.Fatal(err)
		}
		events, err = tm.Due(now)
		if err != nil || len(events) != 1 {
			t.Fatalf("plain Due %v %v", events, err)
		}
		raw, _ = json.Marshal(events[0])
		if strings.Contains(string(raw), "routine_skills") || events[0].Content != ev.Content {
			t.Fatalf("plain event changed: %s", raw)
		}
	}
}
