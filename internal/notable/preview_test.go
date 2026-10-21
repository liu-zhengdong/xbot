package notable

import (
	"context"
	"testing"
)

func TestShellPreviewsDoNotReportCompletedEffects(t *testing.T) {
	for _, cmd := range []string{
		"git clean -n", "git clean -nd", "git clean --dry-run", "git clean --help",
		"git push --dry-run origin main", "git push -n origin main", "git push --force --dry-run origin main",
		"rsync -n a.txt host:/somewhere/x", "rsync --dry-run a.txt host:/somewhere/x",
		`find . -name "-delete"`, `find . -name -delete`, `find . -path "-delete"`,
	} {
		t.Run(cmd, func(t *testing.T) {
			var log Log
			Shell(With(context.Background(), t.TempDir(), &log), cmd, t.TempDir(), nil)
			if items := log.Items(); len(items) != 0 {
				t.Fatalf("preview reported as executed: %+v", items)
			}
		})
	}
}

func TestRealDeleteAndSendStillReport(t *testing.T) {
	for _, tc := range []struct{ cmd, kind string }{{"git clean -fd", "delete"}, {"git push --force origin main", "delete"}, {"rsync a.txt host:/somewhere/x", "external_message"}, {"find . -name old.txt -delete", "delete"}} {
		t.Run(tc.cmd, func(t *testing.T) {
			var log Log
			Shell(With(context.Background(), t.TempDir(), &log), tc.cmd, t.TempDir(), nil)
			items := log.Items()
			if len(items) != 1 || items[0].Kind != tc.kind {
				t.Fatalf("real operation lost: %+v", items)
			}
		})
	}
}
