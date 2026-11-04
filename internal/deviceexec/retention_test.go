package deviceexec

import (
	"fmt"
	"testing"
)

func TestPrunedJobsKeepActualTerminalStatusAndExpireOutput(t *testing.T) {
	m := testManager(t)
	s := testSource()
	original := testCall("retained", "write", "write_file", map[string]any{"path": "kept-file", "content": "kept"})
	accepted := submit(t, m, s, original)
	waitDetail(t, m, s, original.Ref)
	submit(t, m, s, testCall("retained-failure", "read", "read_file", map[string]any{"path": "missing"}))
	waitDetail(t, m, s, "retained-failure")
	for i := 0; i < 21; i++ {
		ref := fmt.Sprintf("later-%d", i)
		submit(t, m, s, testCall(ref, "read", "read_file", map[string]any{"path": "kept-file"}))
		waitDetail(t, m, s, ref)
	}
	m.Close()
	for _, tc := range []struct{ ref, status string }{{"retained", "completed"}, {"retained-failure", "failed"}} {
		d, err := m.Detail(s, tc.ref, 0, 0)
		if err != nil || d.Task.Status != tc.status || !d.OutputExpired {
			t.Fatalf("pruned %s: %+v %v", tc.ref, d, err)
		}
		if _, err := m.Stop(s, tc.ref); err != nil {
			t.Fatalf("terminal stop: %v", err)
		}
	}
	chunk, err := m.Download(s, "retained", "a1", 0, 32)
	if err != nil || string(chunk.Data) != "kept" {
		t.Fatalf("artifact outlives log: %q %v", chunk.Data, err)
	}
	reopened, err := New(m.Root, m.DeviceID, m.DeviceName)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	task := submit(t, reopened, s, original)
	if task.ID != accepted.ID || task.Status != "completed" {
		t.Fatalf("recovered reference: %+v", task)
	}
}
