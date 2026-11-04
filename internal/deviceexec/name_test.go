package deviceexec

import (
	"sync"
	"testing"
)

func TestTaskNameUpdatesWithoutChangingExecutionDevice(t *testing.T) {
	m := testManager(t)
	s := testSource()
	submit(t, m, s, testCall("name", "write", "write_file", map[string]any{"path": "note", "content": "saved"}))
	waitDetail(t, m, s, "name")
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for range 50 {
			m.SetDeviceName("Renamed device")
		}
	}()
	go func() {
		defer workers.Done()
		for range 50 {
			if _, err := m.Detail(s, "name", 0, 0); err != nil {
				t.Error(err)
			}
		}
	}()
	workers.Wait()
	detail, err := m.Detail(s, "name", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.DeviceName != "Renamed device" || detail.Task.DeviceID != "target" {
		t.Fatalf("task device changed: %+v", detail.Task)
	}
	owner := Source{Account: s.Account, Owner: true}
	chunk, err := m.Download(owner, "name", "a1", 0, 32)
	if err != nil || string(chunk.Data) != "saved" {
		t.Fatalf("owner artifact: %q %v", chunk.Data, err)
	}
}
