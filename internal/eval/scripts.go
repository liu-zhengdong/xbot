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
		{Tool: "write_file", Args: args(map[string]string{"path": "site.css", "content": css})},
		{Text: "The page loads the face from site.css. fonts/Text.ttf was missing, so I copied it from fonts/source/Text.ttf."},
	}
}

var passingScripts = map[string]Script{
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
		{Tool: "write_file", Args: args(map[string]string{"path": "view.html", "content": "<p>Chart</p>\n<img src=\"art/plot.svg\">\n"})},
		{Text: "The page loads the drawing from view.html. art/plot.svg was missing, so I copied it from art/source/plot.svg."},
	},
	"meter": {
		{Tool: "read_file", Args: args(map[string]string{"path": "meter.txt"})},
		{Text: "SILENT"},
	},
}
