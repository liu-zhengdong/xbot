package memory

import "testing"

func TestNearDuplicate(t *testing.T) {
	dup := [][2]string{
		// Astr's profile line, reworded on every compaction.
		{"Bot 简介已设为「今晚随便逛逛的小助手」", "Bot 简介已定为「今晚随便逛逛的小助手」"},
		{"Bot 简介已设为「今晚随便逛逛的小助手」", "bot简介已经改为「今晚随便逛逛的小助手」。"},
		{"Bot 简介已设为「今晚随便逛逛的小助手」", Provenance("Bot 简介已改为「今晚随便逛逛的小助手」", "astr", "")},
		{"主人喜欢简短的回答", "主人偏好简短回答"},
		{"Owner prefers short answers.", "owner likes short answers"},
		{"The profile was set to night owl", "profile has been changed to night owl"},
		// One contained in the other.
		{"note-capture 写到 `~/notes/inbox.md`", "约定：note-capture 写到 `~/notes/inbox.md`"},
		{"主人时区 UTC+8", "主人时区 UTC+8。"},
	}
	for _, c := range dup {
		if !NearDuplicate(c[0], c[1]) || !NearDuplicate(c[1], c[0]) {
			t.Errorf("want duplicate: %q / %q", c[0], c[1])
		}
	}
	distinct := [][2]string{
		// Corrections change an anchor and must stay.
		{"Bot 简介已设为「今晚随便逛逛的小助手」", "Bot 简介已改为「严肃的工作助手」"},
		{"主人时区 UTC+8", "主人时区 UTC+9"},
		{"trace 写到 /Users/a/trace/1.json", "trace 写到 /Users/a/trace/2.json"},
		{"主人偏好简短回答", "主人偏好详细回答"},
		{"Owner prefers short answers", "Owner prefers long answers"},
		{"Owner prefers short answers", "Owner no longer prefers short answers"},
		// A short fragment is not a containing match.
		{"主人", "主人每天七点起床"},
		{"", ""},
	}
	for _, c := range distinct {
		if NearDuplicate(c[0], c[1]) || NearDuplicate(c[1], c[0]) {
			t.Errorf("want distinct: %q / %q", c[0], c[1])
		}
	}
}

func TestBareStripsProvenance(t *testing.T) {
	for _, s := range []string{Provenance("x y", "astr", ""), Provenance("x y", "astr", "laptop"), OwnerProvenance("x y", "phone")} {
		if got := Bare(s); got != "x y" {
			t.Fatalf("Bare(%q) = %q", s, got)
		}
	}
}
