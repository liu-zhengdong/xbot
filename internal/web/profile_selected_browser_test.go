package web

import "testing"

// The shipped profile form must update the remembered selection when the
// selected Bot is renamed, so a genuinely new page can reopen that Bot.
func TestProfileRenameSelectedBotSurvivesReopenBrowser(t *testing.T) {
	runSelectedBotBrowser(t, "rename-reopen")
}
