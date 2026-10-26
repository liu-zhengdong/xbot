package bot

import "path/filepath"

// TranscriptDir holds the existing transcript segments and tool-result blobs.
func (b *Bot) TranscriptDir() string { return (&segStore{dir: b.dir()}).transcriptDir() }

// AgentsDir holds the existing per-task directories, not a second trace log.
func (b *Bot) AgentsDir() string { return b.AgentDir("") }

// TracePaths describes this bot's local storage for both scripts and the model.
type TracePaths struct {
	Bot        string `json:"bot"`
	Trace      string `json:"trace"`
	Transcript string `json:"transcript"`
	Agents     string `json:"agents"`
	Access     string `json:"access"`
}

// Paths returns absolute paths without creating any files. Logs and task
// directories may not exist until the first corresponding write.
func (b *Bot) Paths() (TracePaths, error) {
	p := TracePaths{Access: "read_file: WorkDir only; read traces via shell/xbot trace/scripts."}
	for _, pair := range []struct {
		dst  *string
		path string
	}{
		{&p.Bot, b.dir()}, {&p.Trace, b.TracePath()},
		{&p.Transcript, b.TranscriptDir()}, {&p.Agents, b.AgentsDir()},
	} {
		abs, err := filepath.Abs(pair.path)
		if err != nil {
			return TracePaths{}, err
		}
		*pair.dst = abs
	}
	return p, nil
}
