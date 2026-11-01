package cliops

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestProfileProtocolIdentityAndFailures(t *testing.T) {
	root := t.TempDir()
	calls := 0
	s, err := Start(root, Handlers{Profile: func(update *Update) (Profile, error) {
		calls++
		p := Profile{Name: "non-primary", Scope: "runtime", Applied: update != nil}
		if update != nil && update.Description != nil {
			p.Description = *update.Description
		}
		return p, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root, _ = CanonicalRoot(root)
	p, err := Call(context.Background(), s.Endpoint, root, nil)
	if err != nil || p.Name != "non-primary" {
		t.Fatalf("%+v %v", p, err)
	}
	other := t.TempDir()
	if _, err := Call(context.Background(), s.Endpoint, other, nil); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("wrong root: %v", err)
	}
	if calls != 1 {
		t.Fatal("wrong instance reached domain callback")
	}
	desc := "updated"
	p, err = Call(context.Background(), s.Endpoint, root, &Update{Description: &desc})
	if err != nil || p.Description != desc || !p.Applied {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := Call(context.Background(), "", root, nil); err == nil {
		t.Fatal("missing endpoint accepted")
	}
	if _, err := Call(context.Background(), "https://example.com", root, nil); err == nil {
		t.Fatal("nonlocal endpoint accepted")
	}
	env := Caller{Binary: "/current/xbot", Root: root, Endpoint: s.Endpoint, Bot: "non-primary"}.Env([]string{"PATH=/bin", "XBOT_BOT=old", "XBOT_ENDPOINT=old", "XBOT_ROOT=old", "XBOT_BIN=old"})
	want := []string{"PATH=/bin", "XBOT_BIN=/current/xbot", "XBOT_ROOT=" + root, "XBOT_ENDPOINT=" + s.Endpoint, "XBOT_BOT=non-primary"}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("%v", env)
	}
}

// Measures the local protocol, including a fresh connection and root check,
// separately from executable startup. No disk write or model is involved.
func TestProfileIPCOverheadP95(t *testing.T) {
	root := t.TempDir()
	callback := func(*Update) (Profile, error) { return Profile{Name: "ada", Scope: "runtime"}, nil }
	s, err := Start(root, Handlers{Profile: callback})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	samples := make([]time.Duration, 0, 100)
	direct := make([]time.Duration, 0, 100)
	for i := 0; i < 110; i++ {
		start := time.Now()
		_, _ = callback(nil)
		baseline := time.Since(start)
		start = time.Now()
		_, err := Call(context.Background(), s.Endpoint, root, nil)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		if i >= 10 {
			samples = append(samples, elapsed-baseline)
			direct = append(direct, baseline)
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	sort.Slice(direct, func(i, j int) bool { return direct[i] < direct[j] })
	t.Logf("100 local GETs, fresh connection, IPC additional p95=%s; direct callback p95=%s; CLI startup excluded", samples[94], direct[94])
	if samples[94] >= 5*time.Millisecond {
		t.Fatalf("IPC p95 %s exceeds 5ms", samples[94])
	}
}
