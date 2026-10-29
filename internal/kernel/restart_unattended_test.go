package kernel

import (
	"context"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestRestartFromUntrustedEventRunsWithoutCard(t *testing.T) {
	for _, source := range []string{"timer", "webhook"} {
		for _, permissive := range []bool{false, true} {
			t.Run(source+map[bool]string{false: "/cards-on", true: "/cards-off"}[permissive], func(t *testing.T) {
				p := &scripted{resps: []provider.Message{toolCall("restart", "restart_xbot", `{"hint":"继续检查"}`), {Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}}}
				k, b := doorKernel(t, p)
				k.Permissive = permissive
				var hint string
				k.Restart = func(h string) error { hint = h; return nil }
				cards := 0
				k.Approve = func(context.Context, string, string) (bool, error) { cards++; return false, nil }
				if err := k.Handle(context.Background(), link.NewEvent(source, "wake", "运行例程", "external", time.Now())); err != nil {
					t.Fatal(err)
				}
				if hint != "继续检查" || cards != 0 {
					t.Fatalf("restart hint=%q, approval calls=%d", hint, cards)
				}
				entries, err := b.Entries()
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range entries {
					if bot.IsApprovalCard(e.Message.Text()) {
						t.Fatal("restart created an approval card")
					}
				}
			})
		}
	}
}
