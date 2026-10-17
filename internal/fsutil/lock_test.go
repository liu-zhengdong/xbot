package fsutil

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestTryLockHeldElsewhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "held.lock")
	held, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, ok, err := TryLock(path); err != nil || ok {
		t.Fatalf("held: ok=%v err=%v", ok, err)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	l, ok, err := TryLock(path)
	if err != nil || !ok || l == nil {
		t.Fatalf("lock after release: ok=%v err=%v", ok, err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestWithLockRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	if err := WithLock(path+".lock", func() error {
		if err := AppendJSONL(path, map[string]string{"a": "b"}); err != nil {
			return err
		}
		return AppendJSONL(path, map[string]string{"c": "d"})
	}); err != nil {
		t.Fatal(err)
	}
	var got []map[string]string
	err := ReadJSONL(path, func(line []byte) error {
		var m map[string]string
		if err := json.Unmarshal(line, &m); err != nil {
			return err
		}
		got = append(got, m)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0]["a"] != "b" || got[1]["c"] != "d" {
		t.Fatalf("got %v", got)
	}
}
