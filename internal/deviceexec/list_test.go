package deviceexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/desktop"
)

func TestRecentListDoesNotHideRunningCount(t *testing.T) {
	m := testManager(t)
	s := testSource()
	driver := &sequenceDriver{entered: make(chan struct{}), release: make(chan struct{})}
	m.Desktop = &desktop.Link{Root: m.Root, Driver: driver}
	submit(t, m, s, testCall("old-running", "desktop", "desktop_type", map[string]any{"text": "a1"}))
	select {
	case <-driver.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("blocking desktop task did not start")
	}
	for i := 0; i < 201; i++ {
		ref := fmt.Sprintf("read-%03d", i)
		submit(t, m, s, testCall(ref, "read", "read_file", map[string]any{"path": "missing"}))
		waitDetail(t, m, s, ref)
	}
	list := m.List(&s)
	if len(list) != 200 || list[0].Ref != "read-001" || list[199].Ref != "read-200" {
		t.Fatalf("recent list: count=%d first=%+v last=%+v", len(list), list[0], list[len(list)-1])
	}
	if m.RunningCount() != 1 {
		t.Fatalf("running count=%d", m.RunningCount())
	}
	if _, err := m.Stop(s, "old-running"); err != nil {
		t.Fatal(err)
	}
	if m.RunningCount() != 0 {
		t.Fatal("stopped job still counted")
	}
	invalid := testCall("long-description", "read", "read_file", map[string]any{"path": "missing"})
	invalid.Description = strings.Repeat("x", 1025)
	if _, err := m.Submit(context.Background(), s, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("description limit: %v", err)
	}
}
