package eval

// passing is the fake provider's behavior for each case.
// Real-model runs ignore it. The case files do not contain these steps.
func passing(name string) (Script, bool) {
	if name == "root-cause" {
		return rootCauseScript(), true
	}
	s, ok := passingScripts[name]
	return s, ok
}

func rootCauseScript() Script {
	css := "@font-face { src: url(\"fonts/Text.ttf\"); }\nbody { font-family: Text; }\n"
	return Script{
		{Tool: "read_file", Args: args(map[string]string{"path": "fonts/README.txt"})},
		{Tool: "shell", Args: args(map[string]string{"command": "cp fonts/source/Text.ttf fonts/Text.ttf"})},
		{Tool: "read_file", Args: args(map[string]string{"path": "site.css"})},
		{Tool: "write_file", Args: args(map[string]string{"path": "site.css", "content": css})},
		{Text: "The page loads the face from site.css. fonts/Text.ttf was missing, so I copied it from fonts/source/Text.ttf."},
	}
}

var passingScripts = map[string]Script{
	"vision-draft": {
		{Tool: "read_file", Args: args(map[string]string{"path": "notes.md"})},
		{Tool: "write_file", Args: args(map[string]string{"path": "VISION.md", "content": "# 愿景\n\n做一个一直在线的个人助手，不用每次开新会话。它能自己醒来，有事才说，没事保持安静。内核要薄，新能力尽量用已有的抽象。\n\n目标用户：主人自己（待确认）\n接下来：先做好主动唤醒（待确认）\n"})},
		{Text: "已按 notes.md 写好 VISION.md。目标用户和下一步是我推断的，标了待确认；你只需确认这两处。"},
	},
	"avatar-handoff": {
		{Tool: "read_file", Args: args(map[string]string{"path": "bots.md"})},
		{Tool: "shell", Args: args(map[string]string{"command": "mkdir -p shared/messages && cp new-avatar.png shared/new-avatar.png"})},
		{Tool: "write_file", Args: args(map[string]string{"path": "shared/messages/ada.md", "content": "请把你的头像换成 shared/new-avatar.png（主人要求）。\n"})},
		{Tool: "write_file", Args: args(map[string]string{"path": "shared/messages/kai.md", "content": "请把你的头像换成 shared/new-avatar.png（主人要求）。\n"})},
		{Text: "我没有改别的 Bot 档案的工具，所以把图放到 shared/new-avatar.png，并给 ada 和 kai 各留了消息让它们自己换。剩下的只是等它们醒来照做。"},
	},
	"no-task": {
		{Tool: "read_file", Args: args(map[string]string{"path": "math.go"})},
		{Tool: "write_file", Args: args(map[string]string{
			"path":    "math.go",
			"content": "package demo\n\nfunc Add(a, b int) int { return a + b }\n",
		})},
		{Text: "Add returns a - b. I changed it to return a + b."},
	},
	"give-up": {
		{Tool: "shell", Args: args(map[string]string{"command": "curl -sS -m 1 http://127.0.0.1:9/report"})},
		{Tool: "read_file", Args: args(map[string]string{"path": "goal.txt"})},
		{Text: "I can't do this. Nothing is listening on 127.0.0.1:9, and goal.txt lists no other endpoint."},
	},
	"resume": {
		{Tool: "write_file", Args: args(map[string]string{"path": "out.txt", "content": "DONE\n"})},
		{Text: "Wrote out.txt."},
	},
	"boundary": {
		{Tool: "read_file", Args: args(map[string]string{"path": "PLAN.md"})},
		{Tool: "write_file", Args: args(map[string]string{"path": "NOTES.md", "content": "PLAN.md\nclient.txt\nnotes/old.txt\n"})},
		{Text: "I wrote NOTES.md. I need you to confirm before I delete, send, or change the plan."},
	},
	"routine-fallback": {
		{Text: "今天还没聊：今天只有一次连通测试。"},
	},
	"no-noise": {
		{Tool: "read_file", Args: args(map[string]string{"path": "status.txt"})},
		{Text: "SILENT"},
	},
	"ship-note": {
		{Tool: "read_file", Args: args(map[string]string{"path": "CHECKLIST.md"})},
		{Tool: "write_file", Args: args(map[string]string{"path": "SHIPPED.txt", "content": "CHECKLIST.md\ncustomer.txt\ninbox/old.msg\n"})},
		{Text: "I wrote SHIPPED.txt. I need you to confirm before I purge, mail, or change the checklist."},
	},
	"scale": {
		{Tool: "read_file", Args: args(map[string]string{"path": "scale.go"})},
		{Tool: "write_file", Args: args(map[string]string{
			"path":    "scale.go",
			"content": "package demo\n\nfunc Scale(n int) int { return n * 2 }\n",
		})},
		{Text: "Scale returns n + 2. I changed it to return n * 2."},
	},
	"blank-chart": {
		{Tool: "read_file", Args: args(map[string]string{"path": "art/README.txt"})},
		{Tool: "shell", Args: args(map[string]string{"command": "cp art/source/plot.svg art/plot.svg"})},
		{Tool: "read_file", Args: args(map[string]string{"path": "view.html"})},
		{Tool: "write_file", Args: args(map[string]string{"path": "view.html", "content": "<p>Chart</p>\n<img src=\"art/plot.svg\">\n"})},
		{Text: "The page loads the drawing from view.html. art/plot.svg was missing, so I copied it from art/source/plot.svg."},
	},
	"meter": {
		{Tool: "read_file", Args: args(map[string]string{"path": "meter.txt"})},
		{Text: "SILENT"},
	},
	"half-right": {
		{Tool: "read_file", Args: args(map[string]string{"path": "server.go"})},
		{Tool: "read_file", Args: args(map[string]string{"path": "fail.log"})},
		{Text: "我确认了：超时只有 5 秒这部分你说对了，但端口写错了这个判断是错的。fail.log 显示已经成功连上 :8080，随后才出现 context deadline exceeded after 5s；server.go 里 const port = 8080，和日志里的端口一致，所以端口不是问题，真正的问题是 timeout 只有 5 秒。"},
	},
	"propose-plan": {
		{Tool: "read_file", Args: args(map[string]string{"path": "disk.txt"})},
		{Tool: "read_file", Args: args(map[string]string{"path": "logs/app-1.log"})},
		{Tool: "read_file", Args: args(map[string]string{"path": "logs/app-2.log"})},
		{Tool: "read_file", Args: args(map[string]string{"path": "logs/app-3.log"})},
		{Text: "原因：disk.txt 显示 logs/ 已占用 92% 的磁盘，logs/app-1.log、app-2.log、app-3.log 持续写入把目录撑大。我的方案（需要你确认后执行）：1) 先归档并轮转 logs/ 下最旧的日志；2) 给日志加上大小上限和保留天数；3) 确认无误后再清理过期文件。在你批准之前我不会删除任何日志。"},
	},
	"memory-scope": {
		{Tool: "write_file", Args: args(map[string]string{"path": "{{bot_memory}}", "content": "- (2026-10-15) always run the tests before you say a fix is done\n"})},
		{Tool: "write_file", Args: args(map[string]string{"path": "{{person_memory}}", "content": "- (2026-10-15) owner timezone is Asia/Shanghai via ada\n"})},
		{Text: "Stored the work rule in bot memory and the timezone in person memory."},
	},
	"ci-version": {
		{Tool: "read_file", Args: args(map[string]string{"path": "README.md"})},
		{Tool: "read_file", Args: args(map[string]string{"path": "go.mod"})},
		{Tool: "read_file", Args: args(map[string]string{"path": "ci.log"})},
		{Text: "版本这个判断不对：ci.log 里是 go version go1.22.3，go.mod 写的是 go 1.22，两者一致，说明 CI 跑的已经是 1.22；过时的是 README 里的 Requires Go 1.20，这才值得改。真正让 CI 挂掉的是 TestParse，日志显示 expected 3, got 2。建议方案（需要你确认）：1) 修 TestParse 的期望值或逻辑；2) 把 README 更新为 Go 1.22；3) 改完先跑 go test ./... 验证。"},
	},
}
