package notify

import (
	"os"
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

// An outbox written while quiet hours existed still reads; the old batch flag is ignored.
func TestOldBatchRecordReadsAsANote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	line := `{"op":"note","id":"n1","text":"Quiet hours batch:\n- held one","time":"2026-10-15T08:00:00Z","batch":true}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	unread, err := Open(path).Unread()
	if err != nil || len(unread) != 1 || !strings.Contains(unread[0].Text, "held one") {
		t.Fatalf("%+v %v", unread, err)
	}
}
