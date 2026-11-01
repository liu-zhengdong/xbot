//go:build smoke

package smoke

import (
	"net/http"
	"testing"
)

// Independent full-profile increment. Only the provider is scripted; the
// binary, source Shell, bound endpoint and rename are the actual product.
// Keep the accepted ordinary profile/avatar/MCP files unchanged.
func TestCLIProfileFullRuntimeRenameEntry(t *testing.T) {
	cliProfileIsolation(t)
	model := newCLIProfileModel(t)
	const cue = "FULL_PROFILE_RENAME_ENTRY"
	model.add(cue, -1, `"$XBOT_BIN" profile set --self --name FullProfileRenamed --json`)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("FullProfileWorker", "Independent full runtime profile rename acceptance.")
	srv.Send("FullProfileWorker", cue)
	value := cliProfileJSON(t, cliProfileResult(t, model, cue, 0))
	if value["name"] != "FullProfileRenamed" {
		t.Fatalf("self rename did not return the actual new name: %v", value)
	}
	status, _ := srv.do(http.MethodGet, "/api/bots/FullProfileRenamed", nil)
	if status != http.StatusOK {
		t.Fatalf("real renamed Bot unavailable through the existing API: HTTP %d", status)
	}
}
