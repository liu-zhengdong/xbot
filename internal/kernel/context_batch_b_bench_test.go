package kernel

import (
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func BenchmarkBatchBPendingUnchanged(b *testing.B) {
	bt, err := bot.Create(b.TempDir(), "bench", "brief", nil)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if err := bt.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("historical text")}}); err != nil {
			b.Fatal(err)
		}
	}
	if err := bt.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(bot.ApprovalCardText("one", "shell", "run", "local", "echo hi", bot.ApprovalPending))}}); err != nil {
		b.Fatal(err)
	}
	k := &Kernel{Bot: bt}
	k.openApprovals()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(k.openApprovals()) != 1 {
			b.Fatal("card lost")
		}
	}
}
