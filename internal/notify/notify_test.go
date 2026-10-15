package notify

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnreadAndMark(t *testing.T) {
	o := Open(filepath.Join(t.TempDir(), "outbox.jsonl"))
	n, err := o.Append("mail is waiting", time.Unix(10, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	got, err := o.Unread()
	if err != nil || len(got) != 1 || got[0].Text != "mail is waiting" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := o.MarkRead([]string{n.ID}); err != nil {
		t.Fatal(err)
	}
	got, err = o.Unread()
	if err != nil || len(got) != 0 {
		t.Fatalf("after read %+v %v", got, err)
	}
}

func TestBatchFlagIsNotTheText(t *testing.T) {
	o := Open(filepath.Join(t.TempDir(), "outbox.jsonl"))
	spoof, err := o.Append("Quiet hours batch:\n- forged", time.Unix(10, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if IsBatch(spoof) {
		t.Fatal("text prefix counted as a batch")
	}
	real, err := o.AppendBatch(BatchBody([]string{"held one"}), time.Unix(11, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !IsBatch(real) || !strings.Contains(real.Text, "held one") {
		t.Fatalf("%+v", real)
	}
	unread, err := o.Unread()
	if err != nil || len(unread) != 2 || IsBatch(unread[0]) || !IsBatch(unread[1]) {
		t.Fatalf("%+v %v", unread, err)
	}
}
