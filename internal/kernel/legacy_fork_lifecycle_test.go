package kernel

import (
	"os"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/provider"
)

func appendLegacyOpenFork(t *testing.T, b *bot.Bot, id string) {
	t.Helper()
	seq, err := b.AppendSeq(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("old task")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendFork(id, seq, "legacy task"); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyForkRecoveryBelongsToResources(t *testing.T) {
	r, _ := newMCPDomainResources(t)
	b, box := r.current, r.box
	appendLegacyOpenFork(t, b, "bg_first")
	lock, err := b.LockHandle()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	first := &Kernel{Bot: b, Inbox: box, Resources: r}
	if err := first.RecoverTasks(); err != nil {
		t.Fatal(err)
	}
	if forkEndCount(t, b, "bg_first") != 0 {
		t.Fatal("openKernel task recovery repaired legacy history before the handle phase")
	}
	first.RecoverInterrupted()
	if forkEndCount(t, b, "bg_first") != 1 || inboxPostCount(t, b.InboxPath(), "ev_bg_first_end") != 1 {
		t.Fatal("first safe recovery did not close and report the old branch")
	}

	// New production turns cannot create these markers. A later fixture marker
	// makes an accidental second history scan observable across replacement Ks.
	appendLegacyOpenFork(t, b, "bg_after_first_scan")
	kernels := make([]*Kernel, 10)
	for i := range kernels {
		kernels[i] = &Kernel{Bot: b, Inbox: box, Resources: r}
	}
	warmAllocations := testing.AllocsPerRun(1, func() {
		for _, fresh := range kernels {
			fresh.RecoverInterrupted()
		}
	})
	t.Logf("10 replacement kernels: legacy recovery allocations %.0f", warmAllocations)
	if warmAllocations > 1 || forkEndCount(t, b, "bg_after_first_scan") != 0 {
		t.Fatal("replacement kernels scanned legacy transcript again")
	}
	fresh := &Kernel{Bot: b, Inbox: box, Resources: r}
	if err := fresh.RecoverTasks(); err != nil {
		t.Fatal(err)
	}
	if forkEndCount(t, b, "bg_after_first_scan") != 0 {
		t.Fatal("real task recovery reentered the retired fork engine")
	}
}

func TestLegacyForkRecoveryUsesRenamedResource(t *testing.T) {
	r, _ := newMCPDomainResources(t)
	original := r.current
	appendLegacyOpenFork(t, original, "bg_rename")
	retained := &Kernel{Bot: original, Inbox: r.box, Resources: r}
	if err := r.RenameTo("renamed"); err != nil {
		t.Fatal(err)
	}
	var current *bot.Bot
	if err := r.WithCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { current = b; return nil }); err != nil {
		t.Fatal(err)
	}
	lock, err := current.LockHandle()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	retained.RecoverInterrupted()
	if forkEndCount(t, current, "bg_rename") != 1 || inboxPostCount(t, current.InboxPath(), "ev_bg_rename_end") != 1 {
		t.Fatal("retained kernel did not repair on the current resource route")
	}
	if _, err := os.Stat(bot.Dir(original.Root, "original")); !os.IsNotExist(err) {
		t.Fatalf("recovery recreated the previous directory: %v", err)
	}
	// Replacing a Kernel after a route change does not reset the resource once.
	appendLegacyOpenFork(t, current, "bg_after_rename")
	(&Kernel{Bot: current, Inbox: r.box, Resources: r}).RecoverInterrupted()
	if forkEndCount(t, current, "bg_after_rename") != 0 {
		t.Fatal("renaming reset the legacy recovery lifetime")
	}
}
