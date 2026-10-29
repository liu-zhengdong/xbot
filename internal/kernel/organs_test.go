package kernel

import (
	"os"
	"strings"
	"testing"

	"xbot/internal/link"
)

func TestOrgansMountAndRegisterWithoutGrants(t *testing.T) {
	b := testBot(t)
	if err := os.WriteFile(b.Root+"/apps.json", []byte(`{"links":["memory","desktop"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b}
	for _, name := range []string{link.NameTerminal, link.NameTools, link.NameTimer, link.NameMemory, link.NamePersona, link.NameContext, link.NameAgents, link.NameSkills, link.NameWebhook, link.NameWatch, link.NameWeb, link.NamePeers, link.NameDesktop, link.NameCoding} {
		if _, ok := link.LookupOrgan(name); !ok {
			t.Errorf("native capability %s is not in Organs", name)
		}
	}
	for _, organ := range link.Organs() {
		if !namedLink(k.snapshotLinks(), organ.Grant) {
			t.Errorf("organ %s is not mounted", organ.ID)
		}
	}
	for _, name := range []string{"recall", "experience_record", "bot_update_profile", "routine_create", "recall_history", "list_bots", "ask_user", "spawn", "read_skill", "write_skill", "read_file", "web_fetch", "desktop_click", "code_start"} {
		if !registered(k, name) {
			t.Errorf("native tool %s is not registered", name)
		}
	}
	if strings.Contains(linksPrefix([]string{link.NameMemory, link.NameSkills, link.NameWebhook}), "能力框") {
		t.Fatal("native organs appeared in capability box")
	}
	if registered(k, "browser_open") {
		t.Fatal("browser mounted before installation")
	}
	if err := b.GrantLink(link.NameBrowser); err != nil {
		t.Fatal(err)
	}
	if !registered(k, "browser_open") {
		t.Fatal("browser missing after installation")
	}
	if err := b.RevokeLink(link.NameBrowser); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(linksPrefix([]string{link.NameMemory, link.NameSkills, link.NameWebhook}), "能力框") {
		t.Fatal("native organs appeared in capability box")
	}
	if registered(k, "browser_open") {
		t.Fatal("browser remains after removal")
	}
	for _, organ := range link.Organs() {
		if !namedLink(k.snapshotLinks(), organ.Grant) {
			t.Errorf("organ %s lost after link removal", organ.ID)
		}
	}
}
