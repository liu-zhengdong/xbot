package kernel

import (
	"context"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/mcp"
)

func TestResourceCurrentRouteKeepsFreshBindingAfterWakeRecheck(t *testing.T) {
	k, _, _ := freshBot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, k.Bot, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	edited, err := bot.Load(k.Bot.Root, k.Bot.Name())
	if err != nil {
		t.Fatal(err)
	}
	if err := edited.SetBinding("replacement_provider", "replacement_model"); err != nil {
		t.Fatal(err)
	}
	fresh, handle, ok, err := resources.TryCurrentHandle()
	if err != nil || !ok {
		t.Fatalf("wake handle: %v %v", ok, err)
	}
	defer handle.Release()
	if fresh.Config().Model != "replacement_model" {
		t.Fatal("initial wake handle did not load current binding")
	}
	checked, err := resources.CurrentBot()
	if err != nil {
		t.Fatal(err)
	}
	if checked.Config().Provider != fresh.Config().Provider || checked.Config().Model != fresh.Config().Model {
		t.Fatalf("current-route recheck returned stale binding: got %s/%s want %s/%s", checked.Config().Provider, checked.Config().Model, fresh.Config().Provider, fresh.Config().Model)
	}
}
