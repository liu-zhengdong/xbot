package exec

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/work"
)

func TestInterruptCurrentUsesMovedRouteAndSkipsAcceptedOwners(t *testing.T) {
	t.Run("cold recovery uses current directory", func(t *testing.T) {
		f := newMoveBotFixture(t)
		oldAgents := f.agents()
		id := "sa_recover001"
		dir := filepath.Join(oldAgents, id)
		if err := writeDescriptor(dir, Descriptor{Version: 1, ID: id, Bot: f.name, Mode: ModeBrief}); err != nil {
			t.Fatal(err)
		}
		j := &job{id: id, dir: dir, turnSeq: 1}
		if _, err := j.receive(Control{Kind: KindQueue, Text: "do not rerun"}); err != nil {
			t.Fatal(err)
		}
		initial := Meta{ID: id, Status: statusRunning, Mode: ModeBrief, TurnSeq: 1}
		if err := writeMeta(dir, initial); err != nil {
			t.Fatal(err)
		}
		legacyID := "sa_legacy001"
		if err := writeMeta(filepath.Join(oldAgents, legacyID), Meta{ID: legacyID, Mode: ModeBrief, Status: statusQueued}); err != nil {
			t.Fatal(err)
		}
		if moved, err := f.move("after", nil); err != nil || !moved {
			t.Fatalf("move: %v %v", moved, err)
		}
		withIO := func(fn func(string, string, string) error) error {
			if owners.TryLock() {
				owners.Unlock()
				t.Error("current recovery entered route before owners")
			}
			f.route.RLock()
			defer f.route.RUnlock()
			return fn(f.name, f.agents(), work.BotDir(f.root, f.name))
		}
		m, err := InterruptCurrent(id, withIO)
		if err != nil || m.Status != bot.ForkInterrupted || m.ID != id || m.TurnSeq != initial.TurnSeq {
			t.Fatalf("current recovery: %+v %v", m, err)
		}
		if TerminalEventID(m.ID, m.TurnSeq) != TerminalEventID(initial.ID, initial.TurnSeq) {
			t.Fatal("recovery changed the original terminal event identity")
		}
		currentDir := filepath.Join(f.agents(), id)
		if !strings.HasPrefix(m.ResultPath, currentDir+string(filepath.Separator)) {
			t.Fatalf("stale result path: %s", m.ResultPath)
		}
		if _, err := os.Stat(m.ResultPath); err != nil {
			t.Fatal(err)
		}
		before, err := readRecords(currentDir)
		if err != nil {
			t.Fatal(err)
		}
		var dropped, ended int
		for _, entry := range before {
			if entry.Kind == "message_dropped" {
				dropped++
			}
			if entry.Kind == "turn_end" {
				ended++
			}
		}
		if dropped != 1 || ended != 1 {
			t.Fatalf("pending input was replayed or not sealed: dropped=%d end=%d", dropped, ended)
		}
		if _, err := InterruptCurrent(id, withIO); err != nil {
			t.Fatal(err)
		}
		after, err := readRecords(currentDir)
		if err != nil || len(after) != len(before) {
			t.Fatalf("recovery repeated append: %d %d %v", len(before), len(after), err)
		}
		if m, err := InterruptCurrent(legacyID, withIO); err != nil || m.Status != bot.ForkInterrupted || m.TurnSeq != 0 {
			t.Fatalf("legacy current recovery: %+v %v", m, err)
		}
		if _, err := os.Stat(filepath.Dir(oldAgents)); !os.IsNotExist(err) {
			t.Fatalf("recovery recreated the old bot directory: %v", err)
		}
		missing := "sa_missing01"
		if _, err := InterruptCurrent(missing, withIO); !os.IsNotExist(err) {
			t.Fatalf("missing task: %v", err)
		}
		if _, err := os.Stat(filepath.Join(f.agents(), missing)); !os.IsNotExist(err) {
			t.Fatal("recovery invented a new task")
		}
	})

	t.Run("running and queued owners stay accepted", func(t *testing.T) {
		f := newMoveBotFixture(t)
		l := f.local()
		gate := &moveGate{entered: make(chan struct{}), release: make(chan struct{})}
		var release sync.Once
		t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
		l.Prov = gate
		ids := []string{"sa_running01", "sa_waiting01"}
		for _, id := range ids {
			if _, err := l.Start(context.Background(), f.request(id, true)); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case <-gate.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("accepted job did not enter its scripted provider")
		}
		if moved, err := f.move("after", nil); err != nil || !moved {
			t.Fatalf("move accepted owners: %v %v", moved, err)
		}
		for i, id := range ids {
			dir := filepath.Join(f.agents(), id)
			// Remove the PID proof: only the existing owner can prove admission.
			m, err := ReadMeta(dir)
			if err != nil {
				t.Fatal(err)
			}
			m.PID, m.ProcStart = 0, 0
			if err := writeMeta(dir, m); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(metaPath(dir))
			if err != nil {
				t.Fatal(err)
			}
			current, err := InterruptCurrent(id, l.WithCurrentIO)
			want := statusRunning
			if i == 1 {
				want = statusQueued
			}
			if err != nil || current.Status != want || current.ID != id {
				t.Fatalf("accepted owner %s: %+v %v", id, current, err)
			}
			after, err := os.ReadFile(metaPath(dir))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("recovery wrote accepted task metadata", err)
			}
			if _, ok := ActiveTask(dir); !ok {
				t.Fatal("recovery removed actual admission owner")
			}
		}
		release.Do(func() { close(gate.release) })
		for range ids {
			moveTerminal(t, l)
		}
	})

	t.Run("invalid id never enters path callback", func(t *testing.T) {
		if _, err := InterruptCurrent("../outside", func(func(string, string, string) error) error { t.Fatal("invalid id reached path IO"); return nil }); err == nil {
			t.Fatal("invalid task id accepted")
		}
	})
}
