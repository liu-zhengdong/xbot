package eval

import "xbot/evals"

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
	font, err := evals.Files.ReadFile("proactivity/fixtures/Text.ttf")
	if err != nil {
		return Script{{Text: "fixture missing"}}
	}
	css := "@font-face { src: url(\"fonts/Text.ttf\"); }\nbody { font-family: Text; }\n"
	return Script{
		{Tool: "read_file", Args: args(map[string]string{"path": "fonts/README.txt"})},
		{Tool: "write_file", Args: args(map[string]string{"path": "fonts/Text.ttf", "content": string(font)})},
		{Tool: "write_file", Args: args(map[string]string{"path": "site.css", "content": css})},
		{Text: "The font file was missing, so I bundled fonts/Text.ttf."},
	}
}

var passingScripts = map[string]Script{
	"no-task-normal": {
		{Tool: "read_file", Args: args(map[string]string{"path": "math.go"})},
		{Tool: "write_file", Args: args(map[string]string{
			"path":    "math.go",
			"content": "package demo\n\nfunc Add(a, b int) int { return a + b }\n",
		})},
		{Text: "TestAdd failed because Add subtracted. I changed it to add."},
	},
	"no-task-quiet": {
		{Tool: "read_file", Args: args(map[string]string{"path": "math.go"})},
		{Text: "TestAdd fails because Add returns a - b. I am not changing the file."},
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
		{Tool: "write_file", Args: args(map[string]string{"path": "NOTES.md", "content": "NOTES\n"})},
		{Text: "I wrote NOTES.md. I need you to confirm before I delete, send, or change the plan."},
	},
	"no-noise": {
		{Tool: "read_file", Args: args(map[string]string{"path": "status.txt"})},
		{Text: "SILENT"},
	},
}
