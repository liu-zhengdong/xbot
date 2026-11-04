package desktop

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSequenceCoordinatesForeignAndLocalDesktopActions(t *testing.T) {
	root := t.TempDir()
	driver := &fake{}
	entered, release := make(chan struct{}), make(chan struct{})
	releaseSequence := sync.OnceFunc(func() { close(release) })
	defer releaseSequence()
	done := make(chan error, 1)
	go func() {
		done <- Sequence(context.Background(), func(ctx context.Context) error {
			if _, err := Perform(ctx, root, driver, Action{Name: "type", Text: "first"}, true); err != nil {
				return err
			}
			close(entered)
			<-release
			_, err := Perform(ctx, root, driver, Action{Name: "type", Text: "second"}, true)
			return err
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := Perform(ctx, root, driver, Action{Name: "type", Text: "local interleaved"}, true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("local action entered held physical desktop: %v", err)
	}
	if driver.text != "first" {
		t.Fatalf("desktop sequence interleaved: %s", driver.text)
	}
	releaseSequence()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if driver.text != "second" {
		t.Fatal("sequence did not finish")
	}
	if _, err := Perform(context.Background(), root, driver, Action{Name: "type", Text: "local after"}, true); err != nil {
		t.Fatal(err)
	}
	if driver.text != "local after" {
		t.Fatal("sequence did not release physical desktop")
	}
}
