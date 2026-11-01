package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/mcp"
	"xbot/internal/tools"
)

func botControlMake(t *testing.T, root, name string, owners ...string) *bot.Bot {
	t.Helper()
	b, err := bot.Create(root, name, "agreed duties for "+name, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners(owners); err != nil {
		t.Fatal(err)
	}
	return b
}

func botControlKernel(t *testing.T, b *bot.Bot) *Kernel {
	t.Helper()
	ctx := context.Background()
	manager := mcp.NewManager(ctx, mcp.Options{})
	r, err := NewBotResources(ctx, b, manager)
	if err != nil {
		manager.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); manager.Close() })
	return &Kernel{Bot: b, Resources: r}
}

func botControlConfig(t *testing.T, root, name string) bot.Config {
	t.Helper()
	b, err := bot.Load(root, name)
	if err != nil {
		t.Fatal(err)
	}
	return b.Config()
}

func botControlObstructLock(t *testing.T, path string) {
	t.Helper()
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
}

func TestBotControlRuntimeTransferUsesOriginalResources(t *testing.T) {
	for _, oldPinned := range []bool{false, true} {
		t.Run(map[bool]string{false: "old unpinned", true: "old pinned"}[oldPinned], func(t *testing.T) {
			root := t.TempDir()
			a := botControlMake(t, root, "A", "first", "shared")
			b := botControlMake(t, root, "B", "shared", "second")
			if err := bot.SetPrimary(root, a.Name()); err != nil {
				t.Fatal(err)
			}
			// Both original resources and handlers exist before the transfer.
			ka, kb := botControlKernel(t, a), botControlKernel(t, b)
			ha, hb := ka.BotHandlers(), kb.BotHandlers()
			ra, rb := ka.Resources, kb.Resources
			ctx := context.Background()
			if reply, err := hb.Pin(ctx, cliops.BotPin{Name: "A", Pinned: false}); err == nil || reply.Applied || reply.Stage != "validation" {
				t.Fatalf("B must initially lack authority: %+v %v", reply, err)
			}
			for _, pinned := range []bool{true, false} {
				reply, err := ha.Pin(ctx, cliops.BotPin{Name: " B ", Pinned: pinned})
				if err != nil || reply.Name != "B" || reply.Pinned != pinned || !reply.Applied || reply.Stage != "complete" || botControlConfig(t, root, "B").Pinned != pinned {
					t.Fatalf("peer pin/unpin: %+v %v", reply, err)
				}
			}
			if reply, err := ha.Pin(ctx, cliops.BotPin{Name: "A", Pinned: oldPinned}); err != nil || !reply.Applied {
				t.Fatalf("primary self pin/unpin: %+v %v", reply, err)
			}
			reply, err := ha.Primary(ctx, " B ")
			if err != nil || reply.Name != "B" || reply.Primary != "B" || !reply.Applied || reply.Stage != "complete" {
				t.Fatalf("transfer: %+v %v", reply, err)
			}
			if primary, err := bot.Primary(root); err != nil || primary != "B" || !botControlConfig(t, root, "B").Pinned || botControlConfig(t, root, "A").Pinned != oldPinned {
				t.Fatalf("actual pointer/pins: %q %v", primary, err)
			}
			if reply, err := ha.Pin(ctx, cliops.BotPin{Name: "B", Pinned: false}); err == nil || reply.Applied || reply.Stage != "validation" || !botControlConfig(t, root, "B").Pinned {
				t.Fatalf("original A next call must lose authority: %+v %v", reply, err)
			}
			if reply, err := ha.Primary(ctx, "A"); err == nil || reply.Applied || reply.Primary != "" || reply.Stage != "validation" {
				t.Fatalf("original A cannot reclaim pointer: %+v %v", reply, err)
			}
			if reply, err := hb.Pin(ctx, cliops.BotPin{Name: "B", Pinned: false}); err != nil || !reply.Applied || reply.Pinned || botControlConfig(t, root, "B").Pinned {
				t.Fatalf("original B gains authority and may unpin primary: %+v %v", reply, err)
			}
			if reply, err := hb.Primary(ctx, "B"); err != nil || !reply.Applied || reply.Primary != "B" || reply.Stage != "complete" || !botControlConfig(t, root, "B").Pinned {
				t.Fatalf("same primary is a committed pointer write and ensures pin: %+v %v", reply, err)
			}
			if ka.Resources != ra || kb.Resources != rb || ka.Bot != a || kb.Bot != b || a.Name() != "A" || b.Name() != "B" {
				t.Fatal("transfer replaced source identity/resources")
			}
		})
	}
}

func TestBotControlRuntimeLocalVisibilityAndBoundary(t *testing.T) {
	root := t.TempDir()
	a := botControlMake(t, root, "A", "first", "shared", "")
	botControlMake(t, root, "peer", "shared", "foreign")
	botControlMake(t, root, "foreign", "foreign")
	botControlMake(t, root, "orphan")
	botControlMake(t, root, "empty-owner", "")
	if err := bot.SetPrimary(root, "A"); err != nil {
		t.Fatal(err)
	}
	h := botControlKernel(t, a).BotHandlers()
	ctx := context.Background()
	if reply, err := h.Pin(ctx, cliops.BotPin{Name: " peer ", Pinned: true}); err != nil || reply.Name != "peer" || !reply.Applied {
		t.Fatalf("owner intersection must qualify: %+v %v", reply, err)
	}
	for _, name := range []string{"foreign", "orphan", "empty-owner", "peer@device", "missing", "../invalid", " "} {
		before := botControlConfig(t, root, "peer")
		pin, pinErr := h.Pin(ctx, cliops.BotPin{Name: name, Pinned: false})
		primary, primaryErr := h.Primary(ctx, name)
		if pinErr == nil || pin.Applied || pin.Pinned || pin.Stage != "validation" || primaryErr == nil || primary.Applied || primary.Primary != "" || primary.Stage != "validation" {
			t.Fatalf("invisible %q: pin=%+v %v primary=%+v %v", name, pin, pinErr, primary, primaryErr)
		}
		if !reflect.DeepEqual(before, botControlConfig(t, root, "peer")) {
			t.Fatalf("rejected %q changed target", name)
		}
		if current, err := bot.Primary(root); err != nil || current != "A" {
			t.Fatalf("rejected %q changed pointer: %q %v", name, current, err)
		}
	}
	// A source without owners is still allowed to operate on itself.
	orphan := botControlMake(t, t.TempDir(), "self")
	if err := bot.SetPrimary(orphan.Root, orphan.Name()); err != nil {
		t.Fatal(err)
	}
	if reply, err := (&Kernel{Bot: orphan}).pinBotRuntime(ctx, cliops.BotPin{Name: " self ", Pinned: false}); err != nil || !reply.Applied || reply.Name != "self" {
		t.Fatalf("self exception: %+v %v", reply, err)
	}
}

func TestBotControlRuntimeRetainedSourceAfterRename(t *testing.T) {
	root := t.TempDir()
	a := botControlMake(t, root, "A", "owner")
	botControlMake(t, root, "peer", "owner")
	if err := bot.SetPrimary(root, "A"); err != nil {
		t.Fatal(err)
	}
	k := botControlKernel(t, a)
	h := k.BotHandlers()
	if err := k.Resources.RenameTo("renamed"); err != nil {
		t.Fatal(err)
	}
	if k.Bot != a || k.Bot.Name() != "A" {
		t.Fatal("fixture must retain the original source handle")
	}
	ctx := context.Background()
	pin, err := h.Pin(ctx, cliops.BotPin{Name: " renamed ", Pinned: false})
	if err != nil || !pin.Applied || pin.Name != "renamed" || botControlConfig(t, root, "renamed").Pinned {
		t.Fatalf("current self route: %+v %v", pin, err)
	}
	primary, err := h.Primary(ctx, " renamed ")
	if err != nil || !primary.Applied || primary.Primary != "renamed" || !botControlConfig(t, root, "renamed").Pinned {
		t.Fatalf("current name qualification: %+v %v", primary, err)
	}
	if _, err := os.Stat(bot.Dir(root, "A")); !os.IsNotExist(err) {
		t.Fatalf("old source directory revived: %v", err)
	}
}

func TestBotControlRuntimePreservesSupportedConfig(t *testing.T) {
	root := t.TempDir()
	a := botControlMake(t, root, "A", "owner", "other")
	b := botControlMake(t, root, "B", "other")
	if err := bot.SetPrimary(root, "A"); err != nil {
		t.Fatal(err)
	}
	k := botControlKernel(t, a)
	h := k.BotHandlers()
	// Save newer ordinary fields/card/skills through a different loaded handle.
	// The original resource keeps its older snapshot, as in the existing path.
	fresh, err := bot.Load(root, "A")
	if err != nil {
		t.Fatal(err)
	}
	description, zone, seed, role, budget := "new display", "Asia/Shanghai", "display#2", bot.RoleDev, 4321
	if err := fresh.ApplyProfile(bot.ProfileUpdate{Description: &description, Timezone: &zone, AvatarSeed: &seed, Role: &role, BackgroundMaxTime: &budget}); err != nil {
		t.Fatal(err)
	}
	if err := fresh.SetPersonaCard("0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if err := fresh.DisableSkill("saved-skill"); err != nil {
		t.Fatal(err)
	}
	beforeA, beforeB := botControlConfig(t, root, "A"), b.Config()
	ctx := context.Background()
	if reply, err := h.Pin(ctx, cliops.BotPin{Name: "A", Pinned: false}); err != nil || !reply.Applied {
		t.Fatalf("self save: %+v %v", reply, err)
	}
	wantA := beforeA
	wantA.Pinned = false
	if got := botControlConfig(t, root, "A"); !reflect.DeepEqual(got, wantA) {
		t.Fatalf("self save lost supported merged fields:\ngot %+v\nwant %+v", got, wantA)
	}
	if reply, err := h.Primary(ctx, "B"); err != nil || !reply.Applied {
		t.Fatalf("peer primary save: %+v %v", reply, err)
	}
	wantB := beforeB
	wantB.Pinned = true
	if got := botControlConfig(t, root, "B"); !reflect.DeepEqual(got, wantB) {
		t.Fatalf("primary changed default binding or known fields:\ngot %+v\nwant %+v", got, wantB)
	}
	if got := botControlConfig(t, root, "A"); !reflect.DeepEqual(got, wantA) {
		t.Fatalf("transfer changed old primary config: %+v", got)
	}
}

func TestBotControlRuntimeLatePinFailureKeepsTransfer(t *testing.T) {
	root := t.TempDir()
	a := botControlMake(t, root, "A", "owner")
	b := botControlMake(t, root, "B", "owner")
	if err := bot.SetPrimary(root, "A"); err != nil {
		t.Fatal(err)
	}
	ka, kb := botControlKernel(t, a), botControlKernel(t, b)
	ha, hb := ka.BotHandlers(), kb.BotHandlers()
	lock := filepath.Join(bot.Dir(root, "B"), "config.json.lock")
	botControlObstructLock(t, lock)
	ctx := context.Background()
	reply, err := ha.Primary(ctx, " B ")
	if err == nil || reply.Name != "B" || reply.Primary != "B" || !reply.Applied || reply.Stage != "pin" || reply.Error == "" {
		t.Fatalf("late pin failure receipt: %+v %v", reply, err)
	}
	if primary, err := bot.Primary(root); err != nil || primary != "B" || botControlConfig(t, root, "B").Pinned {
		t.Fatalf("actual committed pointer with failed pin: %q %v", primary, err)
	}
	if denied, err := ha.Pin(ctx, cliops.BotPin{Name: "A", Pinned: false}); err == nil || denied.Applied || !botControlConfig(t, root, "A").Pinned {
		t.Fatalf("partial still revokes original A: %+v %v", denied, err)
	}
	if gained, err := hb.Pin(ctx, cliops.BotPin{Name: "A", Pinned: false}); err != nil || !gained.Applied || botControlConfig(t, root, "A").Pinned {
		t.Fatalf("preexisting B gains authority after partial: %+v %v", gained, err)
	}
	if same, err := hb.Primary(ctx, "B"); err == nil || !same.Applied || same.Primary != "B" || same.Stage != "pin" {
		t.Fatalf("same primary late error is still an applied write: %+v %v", same, err)
	}
}

func TestBotControlRuntimePinAndPrimaryLockFailures(t *testing.T) {
	root := t.TempDir()
	a := botControlMake(t, root, "A", "owner")
	if err := bot.SetPrimary(root, "A"); err != nil {
		t.Fatal(err)
	}
	// Reload before live resource creation so its pinned snapshot matches disk.
	var err error
	a, err = bot.Load(root, "A")
	if err != nil {
		t.Fatal(err)
	}
	h := botControlKernel(t, a).BotHandlers()
	botControlObstructLock(t, filepath.Join(bot.Dir(root, "A"), "config.json.lock"))
	ctx := context.Background()
	pin, err := h.Pin(ctx, cliops.BotPin{Name: "A", Pinned: false})
	if err == nil || pin.Applied || pin.Pinned || pin.Stage != "config" || !botControlConfig(t, root, "A").Pinned || a.Config().Pinned {
		t.Fatalf("requested false must not claim durable save; original in-memory assignment remains: %+v %v", pin, err)
	}
	botControlObstructLock(t, filepath.Join(root, "primary.lock"))
	primary, err := h.Primary(ctx, "A")
	if err == nil || primary.Applied || primary.Primary != "" || primary.Stage != "primary" {
		t.Fatalf("plain primary lock entrance: %+v %v", primary, err)
	}
}

func TestBotControlRuntimeColdSourceAndBoundedRedaction(t *testing.T) {
	root := t.TempDir()
	a := botControlMake(t, root, "A")
	if err := bot.SetPrimary(root, "A"); err != nil {
		t.Fatal(err)
	}
	// A cold control operation has no Inbox dependency; a complete bad record
	// must not induce an unrelated open/scan on the nil-resources path.
	if err := os.WriteFile(a.InboxPath(), []byte("{bad-record}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: a, Redact: func(text string) string { return strings.ReplaceAll(text, "SECRET", "masked") }}
	h := k.BotHandlers()
	ctx := context.Background()
	if reply, err := h.Pin(ctx, cliops.BotPin{Name: "A", Pinned: false}); err != nil || !reply.Applied {
		t.Fatalf("cold self write should not read Inbox: %+v %v", reply, err)
	}
	pin, err := h.Pin(ctx, cliops.BotPin{Name: "SECRET", Pinned: false})
	if err == nil || pin.Name != "masked" || strings.Contains(pin.Error, "SECRET") {
		t.Fatalf("source redaction: %+v %v", pin, err)
	}
	for _, name := range []string{"SECRET", strings.Repeat("x", tools.DefaultShellOutputBytes)} {
		pin, pinErr := h.Pin(ctx, cliops.BotPin{Name: name, Pinned: false})
		primary, primaryErr := h.Primary(ctx, name)
		if pinErr == nil || primaryErr == nil {
			t.Fatal("invalid/invisible target unexpectedly succeeded")
		}
		for _, value := range []any{pin, primary} {
			raw, err := json.Marshal(value)
			if err != nil || len(raw)+1 > tools.DefaultShellOutputBytes || strings.Contains(string(raw), "SECRET") {
				t.Fatalf("bounded redacted JSON: %d %v", len(raw)+1, err)
			}
		}
		if len(cliops.FormatBotPin(pin)) > tools.DefaultShellOutputBytes || len(cliops.FormatBotPrimary(primary)) > tools.DefaultShellOutputBytes {
			t.Fatal("human receipt exceeded existing Shell budget")
		}
	}
}
