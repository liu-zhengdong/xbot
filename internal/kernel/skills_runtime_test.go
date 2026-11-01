package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/skill"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func runtimeSkillConfig(t *testing.T, root, name, raw string) {
	t.Helper()
	dir := bot.Dir(root, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSkillReadBoundAndIdentity(t *testing.T) {
	root := t.TempDir()
	for _, slug := range []string{"aaa", "ship"} {
		if err := skill.Write(root, slug, "publish", "body", "not-an-id"); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(skill.Dir(root), "aaa", "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: ship\ntitle: not-an-id\ndescription: alias\n---\nALIAS_BODY"), 0600); err != nil {
		t.Fatal(err)
	}
	info, body, err := readRuntimeSkill(skill.Dir(root), " ship ", nil)
	if err != nil || info.Slug != "aaa" || body != "ALIAS_BODY" {
		t.Fatalf("first sorted name match: %+v %q %v", info, body, err)
	}
	if _, _, err := readRuntimeSkill(skill.Dir(root), "not-an-id", nil); err == nil {
		t.Fatal("title treated as identifier")
	}
	if _, _, err := readRuntimeSkill(skill.Dir(root), "ship", []string{" aaa "}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("resolved slug disabled: %v", err)
	}
	// A different candidate must be capped before parsing even when the requested
	// slug is small; sparse storage avoids allocating the fixture in the test.
	if err := os.Truncate(path, work.ResultMaxBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readRuntimeSkill(skill.Dir(root), "ship", nil); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized non-target candidate: %v", err)
	}
}

func TestRuntimeSkillSourceCaptureAndProjection(t *testing.T) {
	b := testBot(t)
	libraryRoot := t.TempDir()
	body := strings.Repeat("instruction\n", 2000) + "SOURCE_SECRET TAIL_SENTINEL"
	if err := skill.Write(libraryRoot, "ship", "publish SOURCE_SECRET", body); err != nil {
		t.Fatal(err)
	}
	r := &BotResources{current: b, ctx: context.Background()}
	secret := "SOURCE_SECRET"
	k := &Kernel{Bot: b, Resources: r, SkillsDir: skill.Dir(libraryRoot), Redact: func(s string) string {
		return strings.ReplaceAll(s, secret, "[source secret]")
	}}
	handlers := k.SkillHandlers()
	// A later Kernel/configuration cannot redirect the retained source's library,
	// resource ref, or dynamic redactor function.
	other := &BotResources{current: b, ctx: context.Background()}
	k.Resources, k.SkillsDir, k.Redact = other, "not-the-source-library", func(string) string { return "wrong redactor" }
	reply, err := handlers.Read(context.Background(), "ship")
	want := strings.ReplaceAll(body, secret, "[source secret]")
	if err != nil || reply.Body == nil || *reply.Body != want || reply.File != "" || reply.Bytes != int64(len(want)) || strings.Contains(reply.Description, secret) {
		t.Fatalf("complete source read: bytes=%d reply=%+v err=%v", len(want), reply, err)
	}
	// Repoint the captured resource through a real directory rename, retaining
	// the stale source Kernel exactly as an existing endpoint does.
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
	secret = "ROTATED_SECRET"
	body = strings.Repeat("<\n", 14000) + secret + " TAIL_SENTINEL"
	if err := skill.Write(libraryRoot, "ship", "publish", body); err != nil {
		t.Fatal(err)
	}
	reply, err = handlers.Read(context.Background(), "ship")
	if err != nil || reply.Body != nil || reply.File == "" || reply.Access != "local" || strings.Contains(reply.Preview, secret) {
		t.Fatalf("artifact projection: %+v err=%v", reply, err)
	}
	raw, err := os.ReadFile(filepath.Join(work.BotDir(b.Root, current.Name()), filepath.FromSlash(reply.File)))
	want = strings.ReplaceAll(body, secret, "[source secret]")
	if err != nil || string(raw) != want || reply.Bytes != int64(len(want)) {
		t.Fatalf("current full artifact: bytes=%d err=%v", len(raw), err)
	}
	encoded, _ := json.Marshal(reply)
	if len(encoded)+1 > cliops.SkillMaxReply {
		t.Fatalf("reply wire size=%d", len(encoded)+1)
	}
	if _, err := os.Stat(bot.Dir(b.Root, b.Name())); !os.IsNotExist(err) {
		t.Fatalf("old Bot directory revived: %v", err)
	}
	// The source writes the data-root library even when it reads a configured
	// different library, and scope=self follows the captured current ref.
	empty := ""
	written, err := handlers.Write(context.Background(), cliops.SkillWrite{Slug: "empty", Description: "empty", Body: &empty, Scope: "self"})
	if err != nil || !written.Written || written.Target != "renamed" || !reflect.DeepEqual(written.UpdatedBots, []string{"renamed"}) {
		t.Fatalf("current self write: %+v err=%v", written, err)
	}
	if _, body, err := skill.OpenBounded(skill.Dir(b.Root), "empty", 1024); err != nil || body != "" {
		t.Fatalf("root library explicit empty body: %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(skill.Dir(libraryRoot), "empty")); !os.IsNotExist(err) {
		t.Fatalf("write used configured read directory: %v", err)
	}
}

func TestRuntimeSkillReplyWireBoundary(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b, SkillsDir: skill.Dir(b.Root)}
	handlers := k.SkillHandlers()
	body := ""
	candidate := cliops.SkillReadReply{Slug: "ship", Name: "ship", Description: "publish", Body: &body}
	for i := 0; i < 3; i++ {
		raw, _ := json.Marshal(candidate)
		body = strings.Repeat("x", len(body)+tools.DefaultShellOutputBytes-len(raw)-1)
		candidate.Bytes = int64(len(body))
	}
	if err := skill.Write(b.Root, "ship", "publish", body); err != nil {
		t.Fatal(err)
	}
	reply, err := handlers.Read(context.Background(), "ship")
	raw, _ := json.Marshal(reply)
	if err != nil || reply.Body == nil || len(raw)+1 != tools.DefaultShellOutputBytes {
		t.Fatalf("exact final JSON budget: bytes=%d body=%v err=%v", len(raw)+1, reply.Body != nil, err)
	}
	if err := skill.Write(b.Root, "ship", "publish", body+"x"); err != nil {
		t.Fatal(err)
	}
	reply, err = handlers.Read(context.Background(), "ship")
	if err != nil || reply.Body != nil || reply.File == "" {
		t.Fatalf("budget+1 must use artifact: %+v %v", reply, err)
	}
	stored, readErr := os.ReadFile(filepath.Join(work.BotDir(b.Root, b.Name()), filepath.FromSlash(reply.File)))
	raw, _ = json.Marshal(reply)
	if readErr != nil || string(stored) != body+"x" || len(raw)+1 > tools.DefaultShellOutputBytes {
		t.Fatalf("complete artifact and caller-safe reply: bytes=%d readErr=%v replyBytes=%d", len(stored), readErr, len(raw)+1)
	}
	// Metadata is not clipped and mislabeled as a complete read.
	path := filepath.Join(k.SkillsDir, "ship", "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: "+strings.Repeat("n", cliops.SkillMaxReply)+"\ndescription: publish\n---\nbody"), 0600); err != nil {
		t.Fatal(err)
	}
	reply, err = handlers.Read(context.Background(), "ship")
	if err == nil || reply.Body != nil || reply.File != "" || !strings.Contains(reply.Error, "metadata") {
		t.Fatalf("oversized metadata: %+v %v", reply, err)
	}
}

func TestRuntimeSkillScopePreflightAndPartial(t *testing.T) {
	body := "instructions"
	t.Run("legacy-preflight-no-migration", func(t *testing.T) {
		root := t.TempDir()
		raw := `{"style":{"tone":"brief"}}`
		runtimeSkillConfig(t, root, "ada", raw)
		_, err := writeRuntimeSkill(root, skill.Dir(root), "ada", cliops.SkillWrite{Slug: "ship", Description: "publish", Body: &body, Scope: "missing"})
		if err == nil {
			t.Fatal("invalid scope accepted")
		}
		after, err := os.ReadFile(filepath.Join(bot.Dir(root, "ada"), "config.json"))
		if err != nil || string(after) != raw {
			t.Fatalf("preflight migrated config: %q %v", after, err)
		}
		if _, err := os.Stat(skill.Dir(root)); !os.IsNotExist(err) {
			t.Fatalf("invalid preflight created library/lock: %v", err)
		}
	})
	t.Run("unresolvable-configured-name", func(t *testing.T) {
		root := t.TempDir()
		runtimeSkillConfig(t, root, "ada", `{"name":"missing"}`)
		if _, err := writeRuntimeSkill(root, skill.Dir(root), "ada", cliops.SkillWrite{Slug: "ship", Description: "publish", Body: &body}); err == nil {
			t.Fatal("configured name did not undergo original second lookup")
		}
		if _, err := os.Stat(skill.Dir(root)); !os.IsNotExist(err) {
			t.Fatalf("identity failure created library/lock: %v", err)
		}
	})
	t.Run("skip-malformed-keep-empty-name", func(t *testing.T) {
		root := t.TempDir()
		runtimeSkillConfig(t, root, "ada", `{}`)
		runtimeSkillConfig(t, root, "broken", `{`)
		reply, err := writeRuntimeSkill(root, skill.Dir(root), "ada", cliops.SkillWrite{Slug: "ship", Description: "publish", Body: &body, Scope: "self"})
		if err != nil || !reply.Written || reply.Target != "ada" || !reflect.DeepEqual(reply.UpdatedBots, []string{"ada"}) {
			t.Fatalf("legacy config enumeration: %+v %v", reply, err)
		}
	})
	t.Run("truthful-partial", func(t *testing.T) {
		root := t.TempDir()
		runtimeSkillConfig(t, root, "ada", `{"name":"ada"}`)
		runtimeSkillConfig(t, root, "bob", `{"name":"bob"}`)
		if err := os.Mkdir(filepath.Join(bot.Dir(root, "bob"), "config.json.lock"), 0700); err != nil {
			t.Fatal(err)
		}
		reply, err := writeRuntimeSkill(root, skill.Dir(root), "ada", cliops.SkillWrite{Slug: "ship", Description: "publish", Body: &body, Scope: " BoB "})
		if err == nil || !reply.Written || reply.Scope != "BoB" || reply.Target != "bob" || !reflect.DeepEqual(reply.UpdatedBots, []string{"ada"}) {
			t.Fatalf("partial receipt: %+v %v", reply, err)
		}
		if _, got, err := skill.OpenBounded(skill.Dir(root), "ship", 1024); err != nil || got != body {
			t.Fatalf("committed file lost: %q %v", got, err)
		}
		b, err := bot.Load(root, "ada")
		if err != nil || !reflect.DeepEqual(b.DisabledSkills(), []string{"ship"}) {
			t.Fatalf("successful switch lost: %v", err)
		}
	})
}

func TestRuntimeSkillCurrentConfigReadFailure(t *testing.T) {
	for _, state := range []string{"malformed", "missing"} {
		t.Run(state, func(t *testing.T) {
			b := testBot(t)
			if err := skill.Write(b.Root, "ship", "publish", "body"); err != nil {
				t.Fatal(err)
			}
			r := &BotResources{current: b, ctx: context.Background()}
			k := &Kernel{Bot: b, Resources: r, SkillsDir: skill.Dir(b.Root)}
			handlers := k.SkillHandlers()
			path := filepath.Join(bot.Dir(b.Root, b.Name()), "config.json")
			if state == "malformed" {
				if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Rename(path, path+".saved"); err != nil {
				t.Fatal(err)
			}
			if reply, err := handlers.Read(context.Background(), "ship"); err == nil || reply.Body != nil || reply.File != "" {
				t.Fatalf("used cached switches after %s config: %+v %v", state, reply, err)
			}
			if _, err := k.skillInfos(); err == nil {
				t.Fatal("prompt accepted unavailable current switches")
			}
			if _, err := k.readSkill("ship"); err == nil {
				t.Fatal("routine accepted unavailable current switches")
			}
		})
	}
}

func TestRuntimeSkillCurrentDisabledForReadAndPrompt(t *testing.T) {
	b := testBot(t)
	if err := skill.Write(b.Root, "ship", "publish", "body"); err != nil {
		t.Fatal(err)
	}
	r := &BotResources{current: b, ctx: context.Background()}
	k := &Kernel{Bot: b, Resources: r, SkillsDir: skill.Dir(b.Root)}
	handlers := k.SkillHandlers()
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
	manual, err := bot.Load(b.Root, "renamed")
	if err != nil {
		t.Fatal(err)
	}
	for _, disabled := range []bool{true, false} {
		if disabled {
			err = manual.DisableSkill("ship")
		} else {
			err = manual.EnableSkill("ship")
		}
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := handlers.Read(context.Background(), "ship")
		infos, promptErr := k.skillInfos()
		if promptErr != nil || (len(infos) == 0) != disabled || (readErr != nil) != disabled {
			t.Fatalf("manual disabled=%v read=%v prompt=%+v/%v", disabled, readErr, infos, promptErr)
		}
	}
}
