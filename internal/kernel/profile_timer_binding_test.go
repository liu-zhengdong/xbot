package kernel

import (
	"context"
	"os"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/link"
	"xbot/internal/mcp"
)

func TestProfileMountedTimerBindsActualKernelBeforeRename(t *testing.T) {
	k, _, _ := freshBot(t)
	original := k.Bot
	// The real cmd.mount assembles organs with a temporary Kernel that has no
	// Resources. StartOperations must bind the same Timer to the actual source.
	k.Links = MountOrgans(original, nil)
	timer, err := k.timer()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(time.Minute)
	if err := timer.Save(link.Routine{ID: "original-timer", Prompt: "original prompt", At: at.Format(time.RFC3339), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, original, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	k.Resources = resources
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	next := "mounted-timer-moved"
	if err := resources.RenameTo(next); err != nil {
		t.Fatal(err)
	}
	out, err := k.RoutineHandlers().Update(ctx, "original-timer", cliops.RoutinePatch{Prompt: "after rename"})
	if err != nil || !out.Applied || out.ID != "original-timer" || out.Stage != "complete" || out.Error != "" {
		t.Fatalf("update %+v: %v", out, err)
	}
	same, err := k.timer()
	if err != nil || same != timer {
		t.Fatal("the cached Timer was replaced")
	}
	list, err := timer.List()
	if err != nil || len(list) != 1 || list[0].Prompt != "after rename" {
		t.Fatalf("current routine %+v %v", list, err)
	}
	events, err := timer.Due(at)
	if err != nil || len(events) != 1 {
		t.Fatalf("current due events %+v %v", events, err)
	}
	if err := timer.Deliver(ctx, events[0], func(link.Event) error {
		if !resources.route.TryLock() {
			t.Fatal("emit holds current route")
		}
		resources.route.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	saved, err := timer.Load("original-timer")
	if err != nil || saved.Enabled || saved.LastSlot == "" {
		t.Fatalf("current commit %+v %v", saved, err)
	}
	if _, err := os.Stat(bot.Dir(original.Root, original.Name())); !os.IsNotExist(err) {
		t.Fatalf("former Bot directory revived: %v", err)
	}
}
