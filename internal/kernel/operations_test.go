package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func operationEndpoint(k *Kernel) string {
	for _, item := range k.CLIEnvironment(nil) {
		if strings.HasPrefix(item, "XBOT_ENDPOINT=") {
			return strings.TrimPrefix(item, "XBOT_ENDPOINT=")
		}
	}
	return ""
}

func TestRuntimeProfileAvoidsHandleLockAndPublishesAfterSave(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b}
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	handle, err := b.LockHandle()
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Release()
	notified := make(chan string, 1)
	turn := &turn{}
	k.openInterject(turn, &Sink{OnProfile: func(notice ProfileNotice) {
		name := notice.Name
		// This must be safe: publication holds neither operation nor profile lock.
		_ = k.CLIEnvironment(nil)
		desc, _ := b.ProfileValues()
		if desc != "runtime update" {
			t.Error("notification preceded persistence")
		}
		notified <- name
	}})
	defer k.closeInterject(turn, true)
	desc, tz := "runtime update", "Asia/Shanghai"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value, err := cliops.Call(ctx, operationEndpoint(k), b.Root, &cliops.Update{Description: &desc, Timezone: &tz})
	if err != nil || value.Description != desc || value.Timezone != tz {
		t.Fatalf("%+v err=%v", value, err)
	}
	select {
	case name := <-notified:
		if name != b.Name() {
			t.Fatal(name)
		}
	default:
		t.Fatal("profile notification missing")
	}
	if b.Config().Timezone != tz {
		t.Fatal("live kernel did not receive profile")
	}
	// A web PATCH uses a separate handle; self show must refresh from it.
	other, err := bot.Load(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	desc = "web update"
	if err := other.ApplyProfile(bot.ProfileUpdate{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	value, err = cliops.Call(ctx, operationEndpoint(k), b.Root, nil)
	if err != nil || value.Description != desc {
		t.Fatalf("stale runtime show: %+v %v", value, err)
	}
}

func TestRuntimeProfileGuideAppearsInModelRequest(t *testing.T) {
	b := testBot(t)
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("one")}}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2}
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", time.Now())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) == 0 || !strings.Contains(p.reqs[0].Messages[0].Text(), cliProfileGuide) {
		t.Fatal("runtime CLI guide missing from model request")
	}
}
