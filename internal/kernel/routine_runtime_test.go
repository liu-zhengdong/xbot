package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func TestRuntimeRoutineTypedCRUD(t *testing.T) {
	b := testBot(t, inbox.NameTimer)
	tm := &link.Timer{Dir: b.RoutineDir(), Loc: time.FixedZone("source", 8*3600)}
	k := &Kernel{Bot: b, Links: []link.Link{tm}}
	h := k.RoutineHandlers()
	ctx := context.Background()
	skills := []string{" missing ", "missing"}
	created, err := h.Create(ctx, cliops.RoutineCreate{ID: "daily", Name: "  raw name  ", Prompt: " prompt ", Cron: " 0 9 * * * ", Skills: skills})
	if err != nil || !created.Applied || created.Stage != "complete" || created.ID != "daily" {
		t.Fatalf("create: %+v %v", created, err)
	}
	if skills[0] != " missing " {
		t.Fatalf("caller skills mutated: %q", skills)
	}
	r, err := tm.Load("daily")
	if err != nil || r.Name != "  raw name  " || r.Prompt != "prompt" || r.Cron != "0 9 * * *" || r.Zone != "local" || !r.Enabled || !reflect.DeepEqual(r.Skills, []string{"missing", "missing"}) {
		t.Fatalf("persisted: %+v %v", r, err)
	}
	if err := tm.Update("daily", func(r *link.Routine) error { r.LastSlot = "20261101T0100"; return nil }); err != nil {
		t.Fatal(err)
	}
	no := false
	updated, err := h.Update(ctx, "daily", cliops.RoutinePatch{Name: "  replacement  ", Prompt: "  ", Cron: "  ", Enabled: &no})
	if err != nil || !updated.Applied {
		t.Fatalf("update: %+v %v", updated, err)
	}
	r, err = tm.Load("daily")
	if err != nil || r.Enabled || r.Name != "  replacement  " || r.Prompt != "prompt" || r.LastSlot != "20261101T0100" || len(r.Skills) != 2 {
		t.Fatalf("merge/omission: %+v %v", r, err)
	}
	empty, clear := "", []string{}
	if _, err := h.Update(ctx, "daily", cliops.RoutinePatch{Precheck: &empty, Skills: &clear}); err != nil {
		t.Fatal(err)
	}
	listed, err := h.List(ctx)
	if err != nil || listed.Routines == nil || listed.Count != 1 {
		t.Fatalf("list: %+v %v", listed, err)
	}
	view := (*listed.Routines)[0]
	if view.ID != "daily" || view.Name != r.Name || view.Prompt != r.Prompt || view.Cron != r.Cron || view.Zone != r.Zone || view.LastSlot != r.LastSlot || view.Enabled || view.Precheck != "" || len(view.Skills) != 0 {
		t.Fatalf("complete mapping: %+v", view)
	}
	if _, err := h.Create(ctx, cliops.RoutineCreate{ID: "daily", Prompt: "overwritten", Cron: "0 10 * * *"}); err != nil {
		t.Fatal(err)
	}
	r, _ = tm.Load("daily")
	if !r.Enabled || r.Name != "" || r.Prompt != "overwritten" || r.LastSlot != "" {
		t.Fatalf("same-ID overwrite: %+v", r)
	}
	for i := 0; i < 2; i++ {
		reply, err := h.Delete(ctx, "daily")
		if err != nil || !reply.Applied || reply.Stage != "complete" {
			t.Fatalf("delete/desired absence: %+v %v", reply, err)
		}
	}
	listed, err = h.List(ctx)
	if err != nil || listed.Routines == nil || len(*listed.Routines) != 0 || listed.Count != 0 {
		t.Fatalf("empty list: %+v %v", listed, err)
	}
}

func TestRuntimeRoutineOnceBoundaryAndFreshValidation(t *testing.T) {
	b := testBot(t, inbox.NameTimer)
	tm := &link.Timer{Dir: b.RoutineDir()}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	k := &Kernel{Bot: b, Links: []link.Link{tm}, Now: func() time.Time { return now }}
	h := k.RoutineHandlers()
	// The source clock function is captured with the source Timer.
	k.Now = func() time.Time { return now.Add(24 * time.Hour) }
	boundary := now.Add(-link.OneShotWindow).In(time.FixedZone("offset", 8*3600)).Format(time.RFC3339)
	reply, err := h.Create(context.Background(), cliops.RoutineCreate{Prompt: "once", At: "  " + boundary + "  "})
	if err != nil || !reply.Applied || !regexp.MustCompile(`^r[0-9a-f]{8}$`).MatchString(reply.ID) {
		t.Fatalf("boundary/auto id: %+v %v", reply, err)
	}
	r, err := tm.Load(reply.ID)
	if err != nil || r.At != now.Add(-link.OneShotWindow).Format(time.RFC3339) {
		t.Fatalf("UTC once: %+v %v", r, err)
	}
	before, err := os.ReadFile(filepath.Join(tm.Dir, reply.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	failed, err := h.Update(context.Background(), reply.ID, cliops.RoutinePatch{Cron: "0 9 * * *"})
	if err == nil || failed.Applied {
		t.Fatalf("once+cron fresh invariant: %+v %v", failed, err)
	}
	after, err := os.ReadFile(filepath.Join(tm.Dir, reply.ID+".json"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("invalid merge altered definition: %v", err)
	}
	failed, err = h.Create(context.Background(), cliops.RoutineCreate{ID: "past", Prompt: "once", At: " " + now.Add(-link.OneShotWindow-time.Second).Format(time.RFC3339) + " "})
	if err == nil || failed.Applied || !strings.Contains(err.Error(), "past") {
		t.Fatalf("trimmed old cutoff: %+v %v", failed, err)
	}
	if _, err := os.Stat(filepath.Join(tm.Dir, "past.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("past persisted: %v", err)
	}
}

func TestRuntimeRoutineSourceCaptureAndArtifact(t *testing.T) {
	b := testBot(t, inbox.NameTimer)
	r := &BotResources{current: b, ctx: context.Background()}
	tm := &link.Timer{WithDirectoryIO: func(fn func(string) error) error {
		return r.WithCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error { return fn(current.RoutineDir()) })
	}}
	secret := "SOURCE_SECRET"
	k := &Kernel{Bot: b, Resources: r, Links: []link.Link{tm}, Redact: func(s string) string { return strings.ReplaceAll(s, secret, "[source]") }}
	h := k.RoutineHandlers()
	k.Links, k.Resources, k.Redact = nil, &BotResources{}, func(string) string { return "wrong redactor" }
	prompt := strings.Repeat("<full prompt>\n", 1600) + secret + " TAIL_SENTINEL"
	if _, err := h.Create(context.Background(), cliops.RoutineCreate{ID: "large", Name: secret, Prompt: prompt, Cron: "0 9 * * *"}); err != nil {
		t.Fatal(err)
	}
	r.route.Lock()
	if err := bot.Rename(b.Root, b.Name(), "renamed"); err != nil {
		r.route.Unlock()
		t.Fatal(err)
	}
	current, err := bot.Load(b.Root, "renamed")
	if err != nil {
		r.route.Unlock()
		t.Fatal(err)
	}
	r.current = current
	r.route.Unlock()
	listed, err := h.List(context.Background())
	if err != nil || listed.Routines != nil || listed.Count != 1 || listed.Access != "local" || !strings.HasSuffix(listed.File, ".json") {
		t.Fatalf("artifact: %+v %v", listed, err)
	}
	raw, err := os.ReadFile(filepath.Join(work.BotDir(b.Root, current.Name()), filepath.FromSlash(listed.File)))
	if err != nil || int64(len(raw)) != listed.Bytes || strings.Contains(string(raw), secret) {
		t.Fatalf("complete redacted artifact bytes=%d: %v", len(raw), err)
	}
	if len(raw) <= tools.DefaultShellOutputBytes || len(raw) > cliops.RoutineMaxReply {
		t.Fatalf("fixture must fit HTTP but require the Shell artifact: %d", len(raw))
	}
	var full cliops.RoutineListReply
	if err := json.Unmarshal(raw, &full); err != nil {
		t.Fatal(err)
	}
	if full.Count != 1 || full.Routines == nil || (*full.Routines)[0].Prompt != strings.ReplaceAll(prompt, secret, "[source]") || (*full.Routines)[0].Name != "[source]" {
		t.Fatal("full artifact fields lost")
	}
	metadata, _ := json.Marshal(listed)
	if len(metadata)+1 > min(cliops.RoutineMaxReply, tools.DefaultShellOutputBytes) {
		t.Fatalf("metadata exceeded Shell budget: %d", len(metadata)+1)
	}
	if _, err := os.Stat(bot.Dir(b.Root, b.Name())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old directory revived: %v", err)
	}
	// Projection never truncates a definition to make a result artifact fit.
	if err := tm.Update("large", func(r *link.Routine) error { r.Prompt = strings.Repeat("x", work.ResultMaxBytes); return nil }); err != nil {
		t.Fatal(err)
	}
	listed, err = h.List(context.Background())
	if err == nil || listed.Error == "" || listed.File != "" {
		t.Fatalf("oversized artifact silently projected: %+v %v", listed, err)
	}
}

func TestRuntimeRoutinePartialAndUnavailable(t *testing.T) {
	b := testBot(t, inbox.NameTimer)
	tm := &link.Timer{Dir: b.RoutineDir()}
	k := &Kernel{Bot: b, Links: []link.Link{tm}}
	h := k.RoutineHandlers()
	if err := os.MkdirAll(tm.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tm.Dir, ".zones"), []byte("invalid zone index"), 0600); err != nil {
		t.Fatal(err)
	}
	created, err := h.Create(context.Background(), cliops.RoutineCreate{ID: "partial", Prompt: "saved", Cron: "0 9 * * *"})
	if err == nil || !created.Applied || created.Stage != "zone_index" || created.Error == "" {
		t.Fatalf("partial create: %+v %v", created, err)
	}
	updated, err := h.Update(context.Background(), "partial", cliops.RoutinePatch{Prompt: "updated despite index failure"})
	if err == nil || !updated.Applied || updated.Stage != "zone_index" {
		t.Fatalf("partial update: %+v %v", updated, err)
	}
	persisted, err := tm.Load("partial")
	if err != nil || persisted.Prompt != "updated despite index failure" {
		t.Fatalf("partial update not committed: %+v %v", persisted, err)
	}
	for _, id := range []string{"partial", "missing"} {
		reply, err := h.Delete(context.Background(), id)
		if err == nil || !reply.Applied || reply.Stage != "zone_index" {
			t.Fatalf("partial delete %s: %+v %v", id, reply, err)
		}
	}
	missing := (&Kernel{}).RoutineHandlers()
	listed, err := missing.List(context.Background())
	if err == nil || listed.Error != "timer: link is not mounted" {
		t.Fatalf("missing timer list: %+v %v", listed, err)
	}
	for _, call := range []func() (cliops.RoutineWriteReply, error){
		func() (cliops.RoutineWriteReply, error) {
			return missing.Create(context.Background(), cliops.RoutineCreate{})
		},
		func() (cliops.RoutineWriteReply, error) {
			return missing.Update(context.Background(), "x", cliops.RoutinePatch{})
		},
		func() (cliops.RoutineWriteReply, error) { return missing.Delete(context.Background(), "x") },
	} {
		reply, err := call()
		if err == nil || reply.Applied || reply.Error != "timer: link is not mounted" {
			t.Fatalf("missing timer write: %+v %v", reply, err)
		}
	}
}

func TestRuntimeRoutineHumanListBudgetArtifact(t *testing.T) {
	b := testBot(t, inbox.NameTimer)
	tm := &link.Timer{Dir: b.RoutineDir()}
	k := &Kernel{Bot: b, Links: []link.Link{tm}}
	const count = 300
	const at = "2026-11-02T00:00:00Z"
	views := make([]cliops.RoutineView, 0, count)
	var human strings.Builder
	fmt.Fprintf(&human, "count: %d\n", count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("once%03d", i)
		if err := tm.Save(link.Routine{ID: id, Prompt: "p", At: at, Enabled: false}); err != nil {
			t.Fatal(err)
		}
		views = append(views, cliops.RoutineView{ID: id, Prompt: "p", At: at, Enabled: false, Skills: []string{}})
		fmt.Fprintf(&human, "id: %s\nname: \nprompt: p\nprecheck: \nskills: []\ncron: \nat: %s\nenabled: false\nlast_slot: \nzone: \n", id, at)
	}
	compact, err := json.Marshal(cliops.RoutineListReply{Routines: &views, Count: count})
	if err != nil {
		t.Fatal(err)
	}
	if len(compact)+1 >= tools.DefaultShellOutputBytes || human.Len() <= tools.DefaultShellOutputBytes || human.Len() >= cliops.RoutineMaxReply {
		t.Fatalf("fixture must fit compact JSON but overflow human Shell output: json=%d human=%d", len(compact)+1, human.Len())
	}
	if formatted := cliops.FormatRoutineList(cliops.RoutineListReply{Routines: &views, Count: count}); formatted != human.String() {
		t.Fatal("shared formatter changed the complete human list output")
	}
	listed, err := k.RoutineHandlers().List(context.Background())
	if err != nil || listed.File == "" || listed.Routines != nil || listed.Count != count || listed.Access != "local" {
		t.Fatalf("human output requires complete artifact: file=%q inline=%t count=%d json=%d human=%d error=%v", listed.File, listed.Routines != nil, listed.Count, len(compact)+1, human.Len(), err)
	}
	metadata, err := json.Marshal(listed)
	formatted := cliops.FormatRoutineList(listed)
	wantReference := fmt.Sprintf("count: %d\n%s (access: local, bytes: %d)\n", count, listed.File, listed.Bytes)
	if err != nil || len(metadata)+1 > tools.DefaultShellOutputBytes || len(formatted) > tools.DefaultShellOutputBytes || formatted != wantReference {
		t.Fatalf("artifact metadata must fit both formats and retain the full reference: json=%d human=%q error=%v", len(metadata)+1, formatted, err)
	}
	raw, err := os.ReadFile(filepath.Join(work.BotDir(b.Root, b.Name()), filepath.FromSlash(listed.File)))
	if err != nil || int64(len(raw)) != listed.Bytes {
		t.Fatalf("full artifact bytes=%d declared=%d error=%v", len(raw), listed.Bytes, err)
	}
	var full cliops.RoutineListReply
	if err := json.Unmarshal(raw, &full); err != nil || full.Count != count || full.Routines == nil || len(*full.Routines) != count {
		t.Fatalf("full artifact count/records lost: count=%d error=%v", full.Count, err)
	}
	for i, view := range *full.Routines {
		if view.ID != views[i].ID || view.Prompt != "p" || view.At != at || view.Enabled || view.Name != "" || view.Precheck != "" || view.Cron != "" || view.Zone != "" || view.LastSlot != "" || len(view.Skills) != 0 {
			t.Fatalf("artifact record %d lost fields: %+v", i, view)
		}
	}
	t.Logf("compact JSON=%d human=%d reference=%d artifact=%d records=%d", len(compact)+1, human.Len(), len(formatted), len(raw), full.Count)
}

func TestRuntimeRoutinePrecheckNeedsOriginalSource(t *testing.T) {
	b := testBot(t, inbox.NameTimer)
	tm := &link.Timer{Dir: b.RoutineDir()}
	k := &Kernel{Bot: b, Links: []link.Link{tm}}
	h := k.RoutineHandlers()
	command := "printf yes"
	reply, err := h.Create(context.Background(), cliops.RoutineCreate{ID: "denied", Prompt: "check", Cron: "0 9 * * *", Precheck: &command})
	if err == nil || reply.Applied || !strings.Contains(err.Error(), "context unavailable") {
		t.Fatalf("unavailable create review: %+v %v", reply, err)
	}
	if _, err := os.Stat(filepath.Join(tm.Dir, "denied.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreviewed create persisted: %v", err)
	}
	if err := tm.Save(link.Routine{ID: "existing", Prompt: "check", Cron: "0 9 * * *", Precheck: command, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	no := false
	reply, err = h.Update(context.Background(), "existing", cliops.RoutinePatch{Enabled: &no})
	if err == nil || reply.Applied {
		t.Fatalf("omitted effective precheck bypass: %+v %v", reply, err)
	}
	empty := ""
	reply, err = h.Update(context.Background(), "existing", cliops.RoutinePatch{Precheck: &empty, Enabled: &no})
	if err != nil || !reply.Applied {
		t.Fatalf("explicit clear asked for review: %+v %v", reply, err)
	}
	r, err := tm.Load("existing")
	if err != nil || r.Enabled || r.Precheck != "" {
		t.Fatalf("clear/false: %+v %v", r, err)
	}
}
