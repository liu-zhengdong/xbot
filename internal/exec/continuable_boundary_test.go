package exec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/provider"
)

func TestContinuableRejectedColdInputReleasesUnusedScope(t *testing.T) {
	release := make(chan struct{})
	p := continueProvider{func(ctx context.Context, r provider.Request) (provider.Message, error) {
		if strings.HasSuffix(r.Messages[len(r.Messages)-1].Text(), "seed") {
			return answer("seed done"), nil
		}
		select {
		case <-release:
			return answer("done"), nil
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		}
	}}
	l := persistentLocal(t.TempDir(), p)
	persistentStart(t, l, "sa_unused001", "seed")
	terminal(t, l)
	var released atomic.Int32
	l.ReleaseScope = func(r StartRequest) {
		if r.ID == "sa_unused001" {
			released.Add(1)
		}
	}
	for i := 0; i < 9; i++ {
		persistentStart(t, l, fmt.Sprintf("sa_busy%04d", i), "hold")
	}
	if _, err := l.Message(context.Background(), "sa_unused001", Control{Text: "rejected"}); !errors.Is(err, ErrQueueFull) {
		t.Fatal(err)
	}
	if released.Load() != 1 {
		t.Fatalf("unused scope released %d times", released.Load())
	}
	close(release)
	for i := 0; i < 9; i++ {
		terminal(t, l)
	}
}

func TestContinuableConsumedSteerDoesNotParkOnBlocked(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	calls := 0
	p := continueProvider{func(ctx context.Context, r provider.Request) (provider.Message, error) {
		calls++
		if calls == 1 {
			close(entered)
			select {
			case <-release:
				return answer("BLOCKED need a choice"), nil
			case <-ctx.Done():
				return provider.Message{}, ctx.Err()
			}
		}
		if !strings.Contains(r.Messages[len(r.Messages)-1].Text(), "ALREADY_DECIDED") {
			t.Error("consumed decision missing from next request")
		}
		return answer("decision applied"), nil
	}}
	l := persistentLocal(t.TempDir(), p)
	persistentStart(t, l, "sa_blocked01", "first")
	<-entered
	if _, err := l.Message(context.Background(), "sa_blocked01", Control{Text: "ALREADY_DECIDED"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if e := terminal(t, l); e.Kind != EvDone {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestContinuableRepeatedQueuedStopFinalizesOnce(t *testing.T) {
	activeRelease := make(chan struct{})
	entered, finishRelease := make(chan struct{}), make(chan struct{})
	var finalizations atomic.Int32
	p := continueProvider{func(ctx context.Context, _ provider.Request) (provider.Message, error) {
		select {
		case <-activeRelease:
			return answer("done"), nil
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		}
	}}
	l := persistentLocal(t.TempDir(), p)
	l.CurrentCWD = func(r StartRequest) string {
		if r.ID == "sa_stopqueue1" {
			finalizations.Add(1)
			close(entered)
			<-finishRelease
		}
		return "repo"
	}
	persistentStart(t, l, "sa_stophold1", "active")
	persistentStart(t, l, "sa_stopqueue1", "queued")
	stopped := make(chan error, 1)
	go func() { _, err := l.Stop(context.Background(), "sa_stopqueue1"); stopped <- err }()
	<-entered
	second, err := l.Stop(context.Background(), "sa_stopqueue1")
	if err != nil || second.Status != "stopping" {
		t.Fatal(second, err)
	}
	close(finishRelease)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued stop failed to return")
	}
	e := terminal(t, l)
	if e.AgentID != "sa_stopqueue1" || !strings.Contains(e.Text, "cancelled") || finalizations.Load() != 1 {
		t.Fatalf("duplicate finalization: %+v count %d", e, finalizations.Load())
	}
	close(activeRelease)
	terminal(t, l)
}

func TestContinuableFailureKeepsOnlyNewClosingMessage(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	closing, finishRelease := make(chan struct{}), make(chan struct{})
	var snapshots atomic.Int32
	p := continueProvider{func(ctx context.Context, r provider.Request) (provider.Message, error) {
		if strings.HasSuffix(r.Messages[len(r.Messages)-1].Text(), "first") {
			close(entered)
			select {
			case <-release:
				return provider.Message{}, context.DeadlineExceeded
			case <-ctx.Done():
				return provider.Message{}, ctx.Err()
			}
		}
		for _, m := range r.Messages {
			if strings.Contains(m.Text(), "OLD_QUEUE") {
				t.Error("failed turn consumed old pending input")
			}
		}
		return answer("new explicit input"), nil
	}}
	l := persistentLocal(t.TempDir(), p)
	l.CurrentCWD = func(StartRequest) string {
		if snapshots.Add(1) == 1 {
			close(closing)
			<-finishRelease
		}
		return "repo"
	}
	persistentStart(t, l, "sa_failclose1", "first")
	<-entered
	if _, err := l.Message(context.Background(), "sa_failclose1", Control{Kind: KindQueue, Text: "OLD_QUEUE"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-closing
	if _, err := l.Message(context.Background(), "sa_failclose1", Control{Text: "NEW_EXPLICIT"}); err != nil {
		t.Fatal(err)
	}
	close(finishRelease)
	if e := terminal(t, l); e.Kind != EvFailed {
		t.Fatal(e)
	}
	if e := terminal(t, l); e.Kind != EvDone {
		t.Fatal(e)
	}
}
