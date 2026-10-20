//go:build smoke

package smoke

import (
	"strings"
	"testing"
	"time"
)

// TestWakeUp is the smoke case for 「主动唤醒」 (journey and spec in
// docs/dev/smoke.md). Four parts:
//
//   - a routine fires on time and raises one note;
//   - a routine with nothing to say stays silent;
//   - a note raised in quiet hours is held, then released once as a batch;
//   - after a restart a fired slot is not run again and the missed slots are
//     summarized once.
//
// Not covered: how the clock row and the bell are drawn; only the data the
// interface reads is checked. The webhook entry is out of scope.
func TestWakeUp(t *testing.T) {
	stub := StartStub(t)

	t.Run("on time", func(t *testing.T) {
		srv := Start(t, stub)
		srv.CreateBot("ada", "You are ada, the bot in the wake-up smoke test.")

		at := time.Now().Add(30 * time.Second).UTC()
		if r := srv.Send("ada", "SMOKE_ROUTINE NEWS "+at.Format(time.RFC3339)); r.Done != "定好了" {
			t.Fatalf("SMOKE_ROUTINE: done.text %q, want %q", r.Done, "定好了")
		}

		row := waitRoutineRow(t, srv, "ada", "SMOKE_FIRE_NEWS", 90*time.Second)
		got, err := time.Parse(time.RFC3339Nano, row.Time)
		if err != nil {
			t.Fatalf("routine row time %q: %v", row.Time, err)
		}
		if d := got.Sub(at); d > time.Minute || d < -time.Minute {
			t.Fatalf("the clock row is %s away from the scheduled time (旅程「主动唤醒」: 到点后 1 分钟内醒来): row %s, at %s",
				d, got, at)
		}

		WaitFor(t, 60*time.Second, "one NOTE_NEWS", func() (bool, string) {
			return countNotes(srv, "NOTE_NEWS") == 1, dumpNotes(srv.Notes())
		})
		if n := countNotes(srv, "NOTE_NEWS"); n != 1 {
			t.Fatalf("the wake-up raised %d NOTE_NEWS notes, want exactly 1 (旅程「主动唤醒」: 有值得说的事时出现一条通知)", n)
		}
	})

	t.Run("nothing to say", func(t *testing.T) {
		srv := Start(t, stub)
		srv.CreateBot("ada", "You are ada, the bot in the wake-up smoke test.")
		before := len(srv.Notes())

		at := time.Now().Add(30 * time.Second).UTC()
		if r := srv.Send("ada", "SMOKE_ROUTINE QUIET "+at.Format(time.RFC3339)); r.Done != "定好了" {
			t.Fatalf("SMOKE_ROUTINE: done.text %q, want %q", r.Done, "定好了")
		}
		waitRoutineRow(t, srv, "ada", "SMOKE_FIRE_QUIET", 90*time.Second)

		time.Sleep(30 * time.Second)
		if n := len(srv.Notes()); n != before {
			t.Fatalf("a SILENT routine added %d note(s) (旅程「主动唤醒」: 没有时回复 SILENT，不产生通知):\n%s",
				n-before, dumpNotes(srv.Notes()))
		}
	})

	t.Run("quiet hours", func(t *testing.T) {
		srv := Start(t, stub)
		srv.CreateBot("ada", "You are ada, the bot in the wake-up smoke test.")

		// Pin the zone so the window is computed against the clock the bot
		// checks, instead of depending on the machine's fallback zone.
		now := time.Now().UTC()
		srv.PatchBot("ada", map[string]any{
			"timezone":    "UTC",
			"quiet_start": now.Add(-time.Hour).Format("15:04"),
			"quiet_end":   now.Add(time.Hour).Format("15:04"),
		})

		at := now.Add(time.Minute).UTC()
		if r := srv.Send("ada", "SMOKE_ROUTINE NEWS "+at.Format(time.RFC3339)); r.Done != "定好了" {
			t.Fatalf("SMOKE_ROUTINE: done.text %q, want %q", r.Done, "定好了")
		}
		srv.Advance(2 * time.Minute)
		waitRoutineRow(t, srv, "ada", "SMOKE_FIRE_NEWS", 90*time.Second)
		// Wait for the turn to reach notify_owner: only then is the note held,
		// and advancing the clock now cannot race the turn.
		WaitFor(t, 60*time.Second, "the notify call to be held", func() (bool, string) {
			rows := srv.Transcript("ada")
			return countRows(rows, "tool", "held until quiet hours end") == 1, dumpRows(rows)
		})
		if n := countNotes(srv, "NOTE_NEWS"); n != 0 {
			t.Fatalf("a note escaped quiet hours (旅程「主动唤醒」: 免打扰时段不推送):\n%s", dumpNotes(srv.Notes()))
		}

		// Past the window, the held note comes out once as a batch.
		srv.Advance(3 * time.Hour)
		WaitFor(t, 90*time.Second, "one quiet hours batch", func() (bool, string) {
			return countBatch(srv) == 1, dumpNotes(srv.Notes())
		})
		batch := batchNotes(srv)
		if len(batch) != 1 || !strings.Contains(batch[0].Text, "NOTE_NEWS") {
			t.Fatalf("after quiet hours the batch is %v, want one note with NOTE_NEWS (旅程「主动唤醒」: 免打扰时段不推送):\n%s",
				batch, dumpNotes(srv.Notes()))
		}
	})

	t.Run("restart does not rerun", func(t *testing.T) {
		srv := Start(t, stub)
		srv.CreateBot("ada", "You are ada, the bot in the wake-up smoke test.")
		if r := srv.Send("ada", "SMOKE_CRON TICK"); r.Done != "定好了" {
			t.Fatalf("SMOKE_CRON: done.text %q, want %q", r.Done, "定好了")
		}

		// Move just past the next 5-minute slot so the routine fires once.
		next := time.Now().Truncate(5 * time.Minute).Add(5*time.Minute + 10*time.Second)
		srv.Advance(time.Until(next))
		waitRoutineRow(t, srv, "ada", "SMOKE_FIRE_TICK", 90*time.Second)

		// Down for two hours, then back up on the same data root.
		srv.Stop()
		srv.Advance(2 * time.Hour)
		srv.Restart()

		// Wait for the model to answer the summary: the row appears before the
		// turn reaches the stub, so counting rows alone races the turn.
		WaitFor(t, 90*time.Second, "the missed-routine summary to be answered", func() (bool, string) {
			return stub.Missed() == 1, dumpRows(srv.Transcript("ada"))
		})
		rows := srv.Transcript("ada")
		// Two hours is a whole number of 5-minute slots, so the clock lands on
		// a slot and that current slot may run once after the restart. What
		// must not happen is a slot running twice, or the missed slots being
		// backfilled one by one.
		slots := map[string]int{}
		for _, r := range rows {
			if r.Role != "user" || !strings.Contains(r.Content, "SMOKE_FIRE_TICK") {
				continue
			}
			at, err := time.Parse(time.RFC3339Nano, r.Time)
			if err != nil {
				t.Fatalf("routine row time %q: %v", r.Time, err)
			}
			slots[at.UTC().Format("2006-01-02T15:04")]++
		}
		for slot, n := range slots {
			if n > 1 {
				t.Fatalf("slot %s ran %d times after the restart, want once (旅程「主动唤醒」: 重启后已经跑过的不重跑):\n%s",
					slot, n, dumpRows(rows))
			}
		}
		if len(slots) > 2 {
			t.Fatalf("the routine ran %d slots after the restart, want at most the current one (旅程「主动唤醒」: 没有补跑 24 次):\n%s",
				len(slots), dumpRows(rows))
		}
		if n := countUserRows(rows, "Missed routine times"); n != 1 {
			t.Fatalf("got %d missed-routine summaries, want exactly 1 (旅程「主动唤醒」: 停机期间错过的只汇总报一次):\n%s",
				n, dumpRows(rows))
		}
		if n := stub.Missed(); n != 1 {
			t.Fatalf("the model saw %d missed-routine summaries, want 1", n)
		}
	})
}

// waitRoutineRow waits for a routine event row carrying marker and returns it.
func waitRoutineRow(t *testing.T, srv *Server, bot, marker string, d time.Duration) Row {
	t.Helper()
	var row Row
	WaitFor(t, d, "a routine clock row with "+marker, func() (bool, string) {
		rows := srv.Transcript(bot)
		for _, r := range rows {
			// The kernel wraps link events in <untrusted_event>, so the routine
			// block is not at the start of the row; the UI draws the clock line
			// from this timer row.
			if r.Role == "user" && strings.Contains(r.Content, "<routine") && strings.Contains(r.Content, marker) {
				row = r
				return true, dumpRows(rows)
			}
		}
		return false, dumpRows(rows)
	})
	return row
}

// countUserRows counts user rows whose text contains want.
func countUserRows(rows []Row, want string) int {
	n := 0
	for _, r := range rows {
		if r.Role == "user" && strings.Contains(r.Content, want) {
			n++
		}
	}
	return n
}

func countNotes(srv *Server, want string) int {
	n := 0
	for _, note := range srv.Notes() {
		if strings.Contains(note.Text, want) {
			n++
		}
	}
	return n
}

func countBatch(srv *Server) int { return len(batchNotes(srv)) }

func batchNotes(srv *Server) []Note {
	var out []Note
	for _, note := range srv.Notes() {
		if strings.HasPrefix(note.Text, "Quiet hours batch:") {
			out = append(out, note)
		}
	}
	return out
}

func dumpNotes(notes []Note) string {
	var b strings.Builder
	for i, note := range notes {
		b.WriteString(note.Text)
		if i < len(notes)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
