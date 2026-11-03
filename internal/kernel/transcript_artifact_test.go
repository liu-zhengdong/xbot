package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func transcriptRuntimeArtifact(t *testing.T, workspace string, reply cliops.TranscriptReadReply, want string) {
	t.Helper()
	transcriptRuntimeBudget(t, reply, nil)
	if reply.Text != "" || reply.Error != "" || reply.Access != "local" || !regexp.MustCompile(`^results/[0-9a-f]{32}\.json$`).MatchString(reply.File) {
		t.Fatalf("actual artifact metadata: %+v", reply)
	}
	path := filepath.Join(workspace, filepath.FromSlash(reply.File))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	full := cliops.TranscriptReadReply{Text: want}
	wantJSON, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON = append(wantJSON, '\n')
	var decoded cliops.TranscriptReadReply
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, wantJSON) || decoded.Text != want || decoded.File != "" || reply.Bytes != int64(len(raw)) || len(raw) > work.ResultMaxBytes {
		t.Fatalf("complete capped semantic Text artifact differs: actual=%d expected=%d metadata=%d", len(raw), len(wantJSON), reply.Bytes)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 || info.Size() != reply.Bytes {
		t.Fatalf("artifact actual permissions/bytes: info=%v err=%v", info, err)
	}
}

func transcriptRuntimeOldPathsAbsent(t *testing.T, root string) {
	t.Helper()
	for _, old := range []string{bot.Dir(root, "source"), work.BotDir(root, "source")} {
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatalf("reader/artifact revived old source path %s: %v", old, err)
		}
	}
}

func TestTranscriptRuntimeDetailSemantic64KiBArtifactAfterRename(t *testing.T) {
	k, endpoint := transcriptRuntimeFixture(t)
	root := k.Bot.Root
	if _, _, err := work.Open(root, "source"); err != nil {
		t.Fatal(err)
	}
	const heldName, heldSecret = "XBOT_TRANSCRIPT_ARTIFACT_TEST_KEY", "held-transcript-artifact-12345"
	previous, _ := config.Held(heldName)
	config.SetHeld(heldName, heldSecret)
	t.Cleanup(func() { config.SetHeld(heldName, previous) })
	const originalMarker, resourceMarker = "ORIGINAL_LAYER_TOKEN", "RESOURCE_LAYER_TOKEN"
	k.Redact = func(text string) string { return strings.ReplaceAll(text, originalMarker, "[source]") }
	k.Resources.Redact = func(text string) string {
		return strings.ReplaceAll(config.RedactHeld(text), resourceMarker, "[resource]")
	}
	body := "SEMANTIC_CAP_BEGIN " + originalMarker + " " + heldSecret + " " + resourceMarker + " " + strings.Repeat("x", 80<<10) + " UNLIMITED_RAW_TAIL"
	experienceRuntimeAppend(t, k.Bot,
		bot.Entry{Message: toolCall("oversize", "inspect", `{"retain":"original call"}`)},
		bot.Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("oversize", body)}},
	)
	stored, err := k.Bot.Slice(2, 2)
	if err != nil || len(stored) != 1 || stored[0].Blob == "" || strings.Contains(stored[0].Message.Text(), "UNLIMITED_RAW_TAIL") {
		t.Fatalf("fixture did not append an actual oversized blob: count=%d err=%v", len(stored), err)
	}
	e, err := k.RecordExperience("capped detail source", "", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	name := "renamed"
	profile, err := k.ApplyProfileChange(bot.ProfileChange{Name: &name})
	if err != nil || !profile.Moved || profile.Name != name || operationEndpoint(k) != endpoint || k.Bot.Name() != "source" || k.currentName() != name {
		t.Fatalf("real move must retain original source endpoint/handle: %+v err=%v", profile, err)
	}
	original, err := k.Detail(2, 0)
	if err != nil || len(original) != detailCap || !strings.Contains(original, "SEMANTIC_CAP_BEGIN") || strings.Contains(original, "UNLIMITED_RAW_TAIL") {
		t.Fatalf("original whole64KiB semantic cap: bytes=%d err=%v", len(original), err)
	}
	if strings.Contains(original, originalMarker) || !strings.Contains(original, "[source]") || !strings.Contains(original, heldSecret) || !strings.Contains(original, resourceMarker) {
		t.Fatal("original semantic text must apply only the source redactor before boundary scrub")
	}
	held := config.RedactHeld(original)
	if strings.Contains(held, heldSecret) || !strings.Contains(held, "[secret]") || !strings.Contains(held, resourceMarker) {
		t.Fatal("held scrub must preserve the distinct resource token for its own layer")
	}
	want := k.Resources.Redact(held)
	full := cliops.TranscriptReadReply{Text: want}
	if len(cliops.FormatTextResult(full)) <= tools.DefaultShellOutputBytes {
		t.Fatal("fixture must exceed the human 32KiB output budget")
	}
	status, reply := transcriptRuntimeHTTP(t, endpoint, root, "detail", "obsolete-call", cliops.TranscriptDetail{Seq: 1, Experience: e.ID})
	if status != http.StatusOK {
		t.Fatalf("renamed source detail status=%d error=%q", status, reply.Error)
	}
	transcriptRuntimeArtifact(t, work.BotDir(root, name), reply, want)
	if strings.Contains(want, heldSecret) || strings.Contains(want, originalMarker) || strings.Contains(want, resourceMarker) || !strings.Contains(want, "[secret]") || !strings.Contains(want, "[resource]") || !strings.Contains(want, "[source]") {
		t.Fatal("artifact expected text lacks the original/held/resource scrub layers")
	}
	// The same endpoint resolves a current-name self target; no old-name alias
	// is invented, and searching still uses the relocated stored preview.
	found, err := cliops.CallTranscriptSearch(context.Background(), endpoint, root, cliops.TranscriptSearch{Query: "SEMANTIC_CAP_BEGIN", Bot: " renamed "})
	if err != nil || !strings.Contains(found.Text, "2 tool:") || strings.Contains(found.Text, "<untrusted_event") {
		t.Fatalf("current self search after blob move: %q %v", found.Text, err)
	}
	transcriptRuntimeOldPathsAbsent(t, root)
}

func TestTranscriptRuntimeEscapedJSONArtifactMovesBetweenOutputAndWrite(t *testing.T) {
	k, endpoint := transcriptRuntimeFixture(t)
	root := k.Bot.Root
	if _, _, err := work.Open(root, "source"); err != nil {
		t.Fatal(err)
	}
	const heldName, heldSecret = "XBOT_TRANSCRIPT_ESCAPED_TEST_KEY", "held-transcript-control-67890"
	previous, _ := config.Held(heldName)
	config.SetHeld(heldName, heldSecret)
	t.Cleanup(func() { config.SetHeld(heldName, previous) })
	redact := func(text string) string { return strings.ReplaceAll(text, "ORIGINAL_FIXTURE_SECRET", "[original]") }
	k.Redact = redact
	k.Resources.Redact = func(text string) string {
		return strings.ReplaceAll(config.RedactHeld(text), "RESOURCE_FIXTURE_MARKER", "[resource]")
	}
	// The transcript stores the complete existing foreign-event fence as data.
	// Detail must retain that fence and both attachment notes in its semantic
	// text; a result artifact is not a raw blob or attachment-byte export.
	body := inbox.Wrap(inbox.Event{Source: "bot:remote", Type: "transcript", Sender: "remote", Time: k.now(), Content: "ESCAPED_BODY_BEGIN ORIGINAL_FIXTURE_SECRET " + heldSecret + " RESOURCE_FIXTURE_MARKER " + strings.Repeat("\x01", 10000) + " ESCAPED_BODY_END"})
	experienceRuntimeAppend(t, k.Bot, bot.Entry{Images: []string{"diagram.png"}, Audios: []string{"meeting.wav"}, Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("escaped", body)}})
	original, err := k.Detail(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := k.Resources.Redact(config.RedactHeld(original))
	full := cliops.TranscriptReadReply{Text: want}
	wire, err := json.Marshal(full)
	if err != nil || len(want) <= 10<<10 || len(cliops.FormatTextResult(full)) > tools.DefaultShellOutputBytes || len(wire)+1 <= tools.DefaultShellOutputBytes {
		t.Fatalf("escaped JSON dimension must overflow while human fits: text=%d human=%d jsonNL=%d err=%v", len(want), len(cliops.FormatTextResult(full)), len(wire)+1, err)
	}
	for _, piece := range []string{"ESCAPED_BODY_BEGIN", "ESCAPED_BODY_END", `source="bot:remote"`, `type="transcript"`, `sender="remote"`, "\n</untrusted_event>", filepath.Join(work.BotDir(root, "source"), work.PastedDir, "diagram.png"), filepath.Join(work.BotDir(root, "source"), work.VoiceDir, "meeting.wav")} {
		if !strings.Contains(want, piece) {
			t.Fatalf("full original semantic text missing %q", piece)
		}
	}
	// presentHistory calls this existing dependency after its read routes have
	// returned. Move through the public API at that boundary, before the actual
	// result-file IO. Keep callback errors for assertion on the test goroutine.
	var once sync.Once
	var moveErr error
	var moved cliops.Profile
	k.Redact = func(text string) string {
		once.Do(func() {
			name := "renamed"
			moved, moveErr = k.ApplyProfileChange(bot.ProfileChange{Name: &name})
		})
		return redact(text)
	}
	status, reply := transcriptRuntimeHTTP(t, endpoint, root, "detail", "", cliops.TranscriptDetail{Seq: 1})
	if moveErr != nil || !moved.Moved || moved.Name != "renamed" || status != http.StatusOK || operationEndpoint(k) != endpoint {
		t.Fatalf("move between semantic output and artifact: moved=%+v err=%v status=%d reply=%+v", moved, moveErr, status, reply)
	}
	// Preserve exactly the output already assembled, including its original
	// attachment path text, while the file itself uses the new current route.
	transcriptRuntimeArtifact(t, work.BotDir(root, "renamed"), reply, want)
	current, err := k.Detail(1, 0)
	if err != nil || !strings.Contains(current, filepath.Join(work.BotDir(root, "renamed"), work.PastedDir, "diagram.png")) || !strings.Contains(current, filepath.Join(work.BotDir(root, "renamed"), work.VoiceDir, "meeting.wav")) || strings.Contains(current, work.BotDir(root, "source")) || !strings.Contains(current, "ESCAPED_BODY_END") {
		t.Fatalf("next original read did not follow current blob/media route: bytes=%d err=%v", len(current), err)
	}
	transcriptRuntimeOldPathsAbsent(t, root)
}

func TestTranscriptRuntimeArtifactWriteFailureAndBoundedRedactedError(t *testing.T) {
	b := botControlMake(t, t.TempDir(), "source", "owner")
	k := botControlKernel(t, b)
	k.WorkDir = work.BotDir(b.Root, b.Name())
	experienceRuntimeAppend(t, k.Bot, bot.Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("failure", "WRITE_FAILURE_BEGIN "+strings.Repeat("f", 40<<10)+" WRITE_FAILURE_END")}})
	if err := os.MkdirAll(k.WorkDir, 0700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(k.WorkDir, work.ResultDir)
	if err := os.WriteFile(blocker, []byte("results is a regular file"), 0600); err != nil {
		t.Fatal(err)
	}
	const heldName, heldSecret = "XBOT_TRANSCRIPT_ERROR_TEST_KEY", "held-transcript-error-24680"
	previous, _ := config.Held(heldName)
	config.SetHeld(heldName, heldSecret)
	t.Cleanup(func() { config.SetHeld(heldName, previous) })
	k.Redact = func(text string) string {
		if strings.Contains(text, "write read result") {
			return "write read result DISPLAY_ERROR " + heldSecret + " RESOURCE_ERROR_MARKER " + strings.Repeat("\x02", 12000)
		}
		return text
	}
	k.Resources.Redact = func(text string) string {
		return strings.ReplaceAll(config.RedactHeld(text), "RESOURCE_ERROR_MARKER", "[resource]")
	}
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(k.CloseOperations)
	t.Cleanup(func() { bot.CloseStore(b) })
	status, reply := transcriptRuntimeHTTP(t, operationEndpoint(k), k.Bot.Root, "detail", "", cliops.TranscriptDetail{Seq: 1})
	if status < 400 || reply.Error == "" || reply.Text != "" || reply.File != "" || reply.Access != "" || reply.Bytes != 0 || strings.Contains(reply.Error, heldSecret) || strings.Contains(reply.Error, "RESOURCE_ERROR_MARKER") || !strings.Contains(reply.Error, "DISPLAY_ERROR") || !strings.Contains(reply.Error, "[secret]") || !strings.Contains(reply.Error, "[resource]") {
		t.Fatalf("real write failure fabricated artifact or lost bounded redaction: status=%d errorBytes=%d replyFile=%q", status, len(reply.Error), reply.File)
	}
	actual, err := os.ReadFile(blocker)
	if err != nil || string(actual) != "results is a regular file" {
		t.Fatalf("filesystem failure fixture was overwritten: %q %v", actual, err)
	}
}
