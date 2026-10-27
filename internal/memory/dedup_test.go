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
		{"约定：每周五下午整理周报", "每周五下午整理周报"},
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
		// Review 1 P1: a long sentence that changes one name, count, quoted
		// value or negation is a correction, however similar the rest is.
		{"主人要求所有项目的代码审查必须由小王负责", "主人要求所有项目的代码审查必须由小李负责"},
		{"项目名称是「工作助手」", "项目名称是「工作的助手」"},
		{"主人每天早上喝咖啡", "主人每天早上喝咖啡2杯"},
		{"主人每天早上喝咖啡", "主人每天早上喝咖啡两杯"},
		{"主人不喜欢喝咖啡", "主人不喜欢不喝咖啡"},
		{"主人每天早上喝咖啡", "主人每天早上不喝咖啡"},
		{"主人周末早上喝咖啡", "主人每天早上喝咖啡"},
		{"每周五下午整理周报", "周末：每周五下午整理周报"},
		{"部署前必须先在测试环境跑完回归并由张三签字确认", "部署前必须先在测试环境跑完回归并由李四签字确认"},
		{"所有对外发布的文档都必须先经过法务部门审核再发出", "所有对外发布的文档都必须先经过市场部门审核再发出"},
		{"所有对外发布的文档都必须先经过法务部门审核再发出", "所有对外发布的文档都不必先经过法务部门审核再发出"},
		{"Owner wants every pull request reviewed by Alice before merging to main", "Owner wants every pull request reviewed by Bob before merging to main"},
		{"Owner wants every pull request reviewed before merging to main", "Owner wants every pull request reviewed twice before merging to main"},
		{"Owner wants every pull request reviewed before merging to main", "Owner doesn't want every pull request reviewed before merging to main"},
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
