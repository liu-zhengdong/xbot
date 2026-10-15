package notify

import (
	"path/filepath"
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
