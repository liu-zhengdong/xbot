package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/remote"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func runtimeBotMake(t *testing.T, root, name, persona string, owners ...string) *bot.Bot {
	t.Helper()
	b, err := bot.Create(root, name, persona, []string{"terminal", "custom-link"})
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) > 0 {
		if err := b.SetOwners(owners); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func runtimeBotJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeBotListSourceVisibility(t *testing.T) {
	root := t.TempDir()
	source := runtimeBotMake(t, root, "source", "source duties", "first", "second", "")
	runtimeBotMake(t, root, "shared", "  "+strings.Repeat("p", 100)+"  ", "second", "foreign")
	runtimeBotMake(t, root, "foreign", "invisible", "foreign")
	runtimeBotMake(t, root, "orphan", "invisible")
	runtimeBotMake(t, root, "empty-owner", "invisible", "")
	now := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)
	devices := remote.Config{Devices: []remote.Device{
		{Name: "edge", ID: "edge", Person: "second", NoBotMessaging: true},
		{Name: "old", ID: "old", Person: "first"},
		{Name: "foreign", ID: "foreign", Person: "foreign"},
		{Name: "unpaired", ID: "unpaired", Person: "first", UnpairedAt: now},
	}}
	runtimeBotJSON(t, filepath.Join(root, "remote", "devices.json"), devices)
	entries := []map[string]string{{"name": "bad/name"}}
	for i := 1; i <= 64; i++ {
		entries = append(entries, map[string]string{"name": fmt.Sprintf("remote%d", i)})
	}
	seen := now.Add(-7 * 24 * time.Hour)
	for _, device := range devices.Devices {
		at := seen
		if device.Name == "old" {
			at = at.Add(-time.Nanosecond)
		}
		runtimeBotJSON(t, filepath.Join(root, "remote", "dirs", device.ID+".json"), map[string]any{"bots": entries, "received": at})
	}
	k := &Kernel{Bot: source, Now: func() time.Time { return now }}
	h := k.BotHandlers()
	k.Now = func() time.Time { return now.Add(24 * time.Hour) }
	reply, err := h.List(context.Background())
	if err != nil || reply.Local == nil || reply.Remote == nil || reply.Count != 64 || len(*reply.Local) != 1 || len(*reply.Remote) != 63 {
		t.Fatalf("source visibility and remote boundary: %+v %v", reply, err)
	}
	local := (*reply.Local)[0]
	if local.Name != "shared" || local.PersonaPreview != strings.Repeat("p", 80) {
		t.Fatalf("original 80-byte preview: %+v", local)
	}
	for i, entry := range *reply.Remote {
		if entry.Name != fmt.Sprintf("remote%d@edge", i+1) || !entry.Seen.Equal(seen) {
			t.Fatalf("first64 before ValidName, original Seen: %+v", entry)
		}
	}
	empty := runtimeBotMake(t, t.TempDir(), "lonely", "duties")
	listed, err := (&Kernel{Bot: empty}).BotHandlers().List(context.Background())
	if err != nil || listed.Local == nil || listed.Remote == nil || len(*listed.Local) != 0 || len(*listed.Remote) != 0 || listed.Count != 0 {
		t.Fatalf("empty inline arrays: %+v %v", listed, err)
	}
}

type runtimeBotStatusSource struct {
	list       []BotStatus
	steps      []Step
	check      func()
	stepsCalls int
	name       string
	n          int
}

func (s *runtimeBotStatusSource) BotStatuses() []BotStatus {
	if s.check != nil {
		s.check()
	}
	return s.list
}

func (s *runtimeBotStatusSource) BotSteps(name string, n int) []Step {
	if s.check != nil {
		s.check()
	}
	s.stepsCalls++
	s.name, s.n = name, n
	return s.steps[:min(n, len(s.steps))]
}

func TestRuntimeBotInfoDynamicHost(t *testing.T) {
	b := runtimeBotMake(t, t.TempDir(), "source", "duties", "source-owner")
	r := &BotResources{current: b, ctx: context.Background()}
	k := &Kernel{Bot: b, Resources: r, Redact: func(s string) string { return strings.ReplaceAll(s, "SECRET", "masked") }}
	h := k.BotHandlers()
	u := &TokenUse{Prompt: 1, Completion: 2, Cached: 3, TurnCompletion: 4, SessionPrompt: 5, SessionCompletion: 6, SessionCached: 7, Window: 8, Estimated: true}
	status := BotStatus{Bot: "host-only", State: StateAwaiting, Tool: "bash", ToolSummary: "SECRET summary", Since: "2026-11-02T01:00:00Z", Backgrounds: 2, Agents: 3, CompletedUnread: 4, LastMessage: "SECRET message", Todo: "SECRET todo", Tokens: u}
	host := &runtimeBotStatusSource{list: []BotStatus{status, {Bot: "other", State: StateIdle}}, check: func() {
		if !r.route.TryLock() {
			t.Error("status callback called while source route held")
			return
		}
		r.route.Unlock()
	}}
	for i := 0; i < 15; i++ {
		host.steps = append(host.steps, Step{callID: "private-call", description: "private-description", Time: time.Unix(int64(i), 0).UTC(), Tool: "bash", State: StepDone, Summary: "SECRET summary"})
	}
	// Web attaches this source after operations have already published handlers.
	k.Statuses = host
	k.Redact = func(string) string { return "wrong redactor" }
	reply, err := h.Info(context.Background(), cliops.BotInfoQuery{Bot: " host-only ", Steps: 99})
	if err != nil || reply.Bots == nil || reply.Steps == nil || reply.Count != 1 || len(*reply.Steps) != 10 || host.name != "host-only" || host.n != 10 {
		t.Fatalf("dynamic host/named/steps: %+v %v host=%+v", reply, err, host)
	}
	view := (*reply.Bots)[0]
	if view.Bot != status.Bot || view.State != status.State || view.Tool != status.Tool || view.ToolSummary != "masked summary" || view.Since != status.Since || view.Backgrounds != 2 || view.Agents != 3 || view.CompletedUnread != 4 || view.LastMessage != "masked message" || view.Todo != "masked todo" || !reflect.DeepEqual(view.Tokens, &cliops.BotTokenUse{Prompt: 1, Completion: 2, Cached: 3, TurnCompletion: 4, SessionPrompt: 5, SessionCompletion: 6, SessionCached: 7, Window: 8, Estimated: true}) {
		t.Fatalf("complete typed status projection: %+v", view)
	}
	raw, _ := json.Marshal(reply)
	if strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "node") || strings.Contains(string(raw), "stale_seconds") {
		t.Fatalf("private or fabricated fields: %s", raw)
	}
	if step := (*reply.Steps)[0]; !step.Time.Equal(host.steps[0].Time) || step.Tool != "bash" || step.State != StepDone || step.Summary != "masked summary" {
		t.Fatalf("public step projection: %+v", step)
	}
	all, err := h.Info(context.Background(), cliops.BotInfoQuery{Steps: 5})
	if err != nil || all.Count != 2 || host.stepsCalls != 1 || len(*all.Steps) != 0 {
		t.Fatalf("whole host view, no ambiguous steps: %+v %v", all, err)
	}
	host.list = host.list[:1]
	if _, err := h.Info(context.Background(), cliops.BotInfoQuery{Steps: 2}); err != nil || host.stepsCalls != 2 || host.n != 2 {
		t.Fatalf("singleton steps: %v %+v", err, host)
	}
	if reply, err := h.Info(context.Background(), cliops.BotInfoQuery{Bot: "missing"}); err == nil || !strings.Contains(reply.Error, "not a same-owner bot") {
		t.Fatalf("unknown named target: %+v %v", reply, err)
	}
	k.Statuses = nil
	fallback, err := h.Info(context.Background(), cliops.BotInfoQuery{})
	if err != nil || fallback.Count != 1 || (*fallback.Bots)[0].Bot != b.Name() {
		t.Fatalf("original fallback: %+v %v", fallback, err)
	}
}

func TestRuntimeBotCreateInheritanceAndPartial(t *testing.T) {
	root := t.TempDir()
	b := runtimeBotMake(t, root, "source", "duties", "first", "second")
	if err := bot.SetPrimary(root, b.Name()); err != nil {
		t.Fatal(err)
	}
	if err := b.InheritBinding(bot.Config{Provider: "source-provider", Model: "source-model", ContextLimit: 1234}); err != nil {
		t.Fatal(err)
	}
	sourceConfig := b.Config()
	sourceConfig.Role, sourceConfig.BackgroundMaxTime = bot.RoleDev, 12345
	runtimeBotJSON(t, filepath.Join(bot.Dir(root, b.Name()), "config.json"), sourceConfig)
	b, err := bot.Load(root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	def, err := config.EnsureDefault(root)
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b}
	h := k.BotHandlers()
	ctx := context.Background()
	for _, in := range []cliops.BotCreate{{Name: "empty", Persona: "  "}, {Name: "../bad", Persona: "duties"}} {
		reply, err := h.Create(ctx, in)
		if err == nil || reply.Applied || reply.Stage != "validation" {
			t.Fatalf("validation: %+v %v", reply, err)
		}
	}
	made, err := h.Create(ctx, cliops.BotCreate{Name: " created ", Persona: "  agreed duties  "})
	if err != nil || made.Name != "created" || !made.Applied || made.Stage != "complete" {
		t.Fatalf("create: %+v %v", made, err)
	}
	created, err := bot.Load(root, "created")
	if err != nil {
		t.Fatal(err)
	}
	cfg := created.Config()
	if cfg.Persona != "agreed duties" || !reflect.DeepEqual(cfg.Links, b.Config().Links) || !reflect.DeepEqual(cfg.Owners, b.Config().Owners) || cfg.Provider != def.Name || cfg.Model != def.Models[0].Name || cfg.ContextLimit != 0 || cfg.Role != "" || cfg.BackgroundMaxTime != 0 {
		t.Fatalf("source grants and global default binding: %+v", cfg)
	}
	if persona, err := created.ReadPersona(); err != nil || persona != "agreed duties" {
		t.Fatalf("actual persona: %q %v", persona, err)
	}
	failed, err := h.Create(ctx, cliops.BotCreate{Name: "partial", Persona: strings.Repeat("p", (32<<10)+1)})
	if err == nil || !failed.Applied || failed.Stage != "persona" || failed.Error == "" {
		t.Fatalf("late 32KiB partial: %+v %v", failed, err)
	}
	for _, name := range []string{"config.json", "state.json"} {
		if _, err := os.Stat(filepath.Join(bot.Dir(root, "partial"), name)); err != nil {
			t.Fatalf("late persona failure lost %s: %v", name, err)
		}
	}
	// The captured redactor runs once for the input name and again after Create
	// succeeds. Use that existing boundary to inject a real owners-save IO fault.
	nameCalls := 0
	var faultErr error
	k.Redact = func(s string) string {
		if s == "owners-partial" {
			nameCalls++
			if nameCalls == 2 {
				lock := filepath.Join(bot.Dir(root, s), "config.json.lock")
				faultErr = os.Rename(lock, lock+".saved")
				if faultErr == nil {
					faultErr = os.Mkdir(lock, 0700)
				}
			}
		}
		return s
	}
	ownerReply, ownerErr := k.BotHandlers().Create(ctx, cliops.BotCreate{Name: "owners-partial", Persona: "agreed duties"})
	if faultErr != nil || ownerErr == nil || !ownerReply.Applied || ownerReply.Stage != "owners" {
		t.Fatalf("actual owners write failure: %+v %v fault=%v", ownerReply, ownerErr, faultErr)
	}
	if saved, err := bot.Load(root, "owners-partial"); err != nil || len(saved.Config().Owners) != 0 {
		t.Fatalf("owners partial must preserve the created bot: %v", err)
	}
	if err := bot.SetPrimary(root, "created"); err != nil {
		t.Fatal(err)
	}
	denied, err := h.Create(ctx, cliops.BotCreate{Name: "no-longer-primary", Persona: "duties"})
	if err == nil || denied.Applied || denied.Stage != "validation" {
		t.Fatalf("current primary recheck: %+v %v", denied, err)
	}
	if _, err := os.Stat(bot.Dir(root, "no-longer-primary")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nonprimary created target: %v", err)
	}
}

func TestRuntimeBotDeleteCallbackResults(t *testing.T) {
	root := t.TempDir()
	b := runtimeBotMake(t, root, "source", "duties", "first")
	runtimeBotMake(t, root, "target", "duties", "first")
	runtimeBotMake(t, root, "foreign", "duties", "other")
	if err := bot.SetPrimary(root, b.Name()); err != nil {
		t.Fatal(err)
	}
	r := &BotResources{current: b, ctx: context.Background()}
	calls := 0
	remove := false
	r.Delete = func(ctx context.Context, name string) (bool, error) {
		calls++
		if !r.route.TryLock() {
			t.Error("host deletion called while source route held")
			return false, errors.New("locked source route")
		}
		r.route.Unlock()
		if name != "target" || ctx.Err() != nil {
			t.Fatalf("original host callback: %s %v", name, ctx.Err())
		}
		if !remove {
			return false, errors.New("host failed before deletion")
		}
		removed, err := bot.DeleteWithResult(root, name)
		if err != nil {
			return removed, err
		}
		return removed, errors.New("vault cleanup failed")
	}
	k := &Kernel{Bot: b, Resources: r, Permissive: true}
	h := k.BotHandlers()
	ctx := context.Background()
	for _, name := range []string{"foreign", "target@device", b.Name()} {
		reply, err := h.Delete(ctx, name)
		if err == nil || reply.Removed || reply.Stage != "validate" || calls != 0 {
			t.Fatalf("local visibility/protection: %+v %v calls=%d", reply, err, calls)
		}
	}
	k.Permissive = false
	reply, err := h.Delete(ctx, "target")
	if err == nil || reply.Removed || reply.Stage != "approval" || calls != 0 {
		t.Fatalf("strict missing source must fail before host: %+v %v calls=%d", reply, err, calls)
	}
	k.Permissive = true
	reply, err = h.Delete(ctx, " target ")
	if err == nil || reply.Removed || reply.Stage != "delete" || calls != 1 {
		t.Fatalf("honest false callback error: %+v %v calls=%d", reply, err, calls)
	}
	remove = true
	reply, err = h.Delete(ctx, "target")
	if err == nil || !reply.Removed || reply.Stage != "vault_key" || calls != 2 {
		t.Fatalf("removed plus late failure: %+v %v calls=%d", reply, err, calls)
	}
	if _, err := bot.Load(root, "target"); !errors.Is(err, bot.ErrNotFound) {
		t.Fatalf("removed target resurrected: %v", err)
	}
	runtimeBotMake(t, root, "fallback", "duties", "first")
	plain := (&Kernel{Bot: b, Permissive: true}).BotHandlers()
	reply, err = plain.Delete(ctx, "fallback")
	if err != nil || !reply.Removed || reply.Stage != "complete" {
		t.Fatalf("original DeleteWithResult fallback: %+v %v", reply, err)
	}
}

func TestRuntimeBotArtifactsCurrentRoute(t *testing.T) {
	root := t.TempDir()
	b := runtimeBotMake(t, root, "source", "duties", "owner")
	runtimeBotMake(t, root, "visible", "EXPAND", "owner")
	r := &BotResources{current: b, ctx: context.Background()}
	expanded := strings.Repeat("complete content <>& ", 2400) + "TAIL_SENTINEL"
	k := &Kernel{Bot: b, Resources: r, Redact: func(s string) string { return strings.ReplaceAll(s, "EXPAND", expanded) }}
	h := k.BotHandlers()
	// The source listener continues to use the original resource ref and redactor.
	k.Redact = func(string) string { return "wrong redactor" }
	r.route.Lock()
	if err := bot.Rename(root, b.Name(), "renamed"); err != nil {
		r.route.Unlock()
		t.Fatal(err)
	}
	current, err := bot.Load(root, "renamed")
	if err != nil {
		r.route.Unlock()
		t.Fatal(err)
	}
	r.current = current
	r.route.Unlock()
	list, err := h.List(context.Background())
	if err != nil || list.Local != nil || list.Remote != nil || list.Count != 1 || list.Access != "local" || list.File == "" {
		t.Fatalf("complete list projection: %+v %v", list, err)
	}
	raw, err := os.ReadFile(filepath.Join(work.BotDir(root, current.Name()), filepath.FromSlash(list.File)))
	if err != nil || int64(len(raw)) != list.Bytes || raw[len(raw)-1] != '\n' {
		t.Fatalf("current resource artifact and real bytes: %d %v", len(raw), err)
	}
	var complete cliops.BotListReply
	if err := json.Unmarshal(raw, &complete); err != nil || complete.Local == nil || (*complete.Local)[0].PersonaPreview != expanded || complete.Remote == nil {
		t.Fatalf("complete redacted list artifact: %+v %v", complete, err)
	}
	if _, err := os.Stat(work.BotDir(root, "source")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale workspace recreated: %v", err)
	}
	encoded, _ := json.Marshal(list)
	if len(encoded)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatBotList(list)) > tools.DefaultShellOutputBytes {
		t.Fatal("list metadata exceeds either final format budget")
	}
	k.Statuses = &runtimeBotStatusSource{list: []BotStatus{{Bot: current.Name(), State: StateRunning, LastMessage: "EXPAND"}}}
	info, err := h.Info(context.Background(), cliops.BotInfoQuery{})
	if err != nil || info.Bots != nil || info.Steps != nil || info.Access != "local" || info.Count != 1 {
		t.Fatalf("complete info projection: %+v %v", info, err)
	}
	raw, err = os.ReadFile(filepath.Join(work.BotDir(root, current.Name()), filepath.FromSlash(info.File)))
	if err != nil || int64(len(raw)) != info.Bytes {
		t.Fatalf("info artifact: %v", err)
	}
	var completeInfo cliops.BotInfoReply
	if err := json.Unmarshal(raw, &completeInfo); err != nil || completeInfo.Bots == nil || (*completeInfo.Bots)[0].LastMessage != expanded || completeInfo.Steps == nil {
		t.Fatalf("complete info fields: %+v %v", completeInfo, err)
	}
	encoded, _ = json.Marshal(info)
	if len(encoded)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatBotInfo(info)) > tools.DefaultShellOutputBytes {
		t.Fatal("info metadata exceeds either final format budget")
	}
	// Empty token counters omit their JSON fields but still render a complete
	// human usage line. This makes the human format alone exceed the Shell cap.
	statuses := make([]BotStatus, 256)
	views := make([]cliops.BotStatusView, len(statuses))
	for i := range statuses {
		name := fmt.Sprintf("bot%d", i)
		statuses[i] = BotStatus{Bot: name, State: StateIdle, Tokens: &TokenUse{}}
		views[i] = cliops.BotStatusView{Bot: name, State: StateIdle, Tokens: &cliops.BotTokenUse{}}
	}
	emptySteps := []cliops.BotStepView{}
	full := cliops.BotInfoReply{Bots: &views, Steps: &emptySteps, Count: len(views)}
	encoded, _ = json.Marshal(full)
	if len(encoded)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatBotInfo(full)) <= tools.DefaultShellOutputBytes {
		t.Fatal("fixture must isolate human-only overflow")
	}
	k.Statuses = &runtimeBotStatusSource{list: statuses}
	humanOnly, err := h.Info(context.Background(), cliops.BotInfoQuery{})
	if err != nil || humanOnly.File == "" || humanOnly.Bots != nil || humanOnly.Count != len(statuses) {
		t.Fatalf("human-only overflow was not projected: %+v %v", humanOnly, err)
	}
}

func TestRuntimeBotCompleteOversizeRejected(t *testing.T) {
	b := runtimeBotMake(t, t.TempDir(), "source", "duties")
	k := &Kernel{Bot: b, Statuses: &runtimeBotStatusSource{list: []BotStatus{{Bot: b.Name(), State: StateIdle, LastMessage: strings.Repeat("x", work.ResultMaxBytes)}}}, Redact: func(s string) string { return s }}
	reply, err := k.BotHandlers().Info(context.Background(), cliops.BotInfoQuery{})
	if err == nil || reply.File != "" || reply.Bots != nil || !strings.Contains(reply.Error, "over") {
		t.Fatalf("complete oversize must fail: %+v %v", reply, err)
	}
	if _, err := os.Stat(filepath.Join(work.BotDir(b.Root, b.Name()), work.ResultDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversize created a truncated result: %v", err)
	}
	// A canceled request cannot enter filesystem work or host callbacks.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := k.BotHandlers().List(ctx); err == nil {
		t.Fatal("canceled source read accepted")
	}
}
