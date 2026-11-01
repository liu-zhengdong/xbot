package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/mcp"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/skill"
	"xbot/internal/vault"
	"xbot/internal/work"
)

// Retain a different Kernel/Bot handle across a real resource move. Captured
// callbacks and cached sidecars must follow that resource, not reopen before/.
func TestRetainedKernelLocalPathsFollowRename(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	b, err := bot.Create(root, "before", "fixture persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := bot.SetPrimary(root, "before"); err != nil {
		t.Fatal(err)
	}
	other, err := bot.Create(root, "other", "other fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	oldWork, shared, err := work.Open(root, "before")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(oldWork, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bot.Dir(root, "before"), "cwd"), []byte(filepath.Join(oldWork, "nested")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("kernel-path-marker")}}); err != nil {
		t.Fatal(err)
	}
	// Configure only a local credential fixture; resolving the Save callback
	// never invokes the image provider or a real model.
	on, off := true, false
	if err := config.Add(root, config.Provider{Name: "images", Protocol: config.Responses, BaseURL: "http://127.0.0.1:1", OAuth: "chatgpt"}, config.ModelSpec{Name: "fixture-model", Context: 1000, HostedImageGen: &on, HostedSearch: &off}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetBinding("images", "fixture-model"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWebImageGen(root, true, "openai", "auto"); err != nil {
		t.Fatal(err)
	}
	v, err := config.OAuthVault(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.SetOAuth(v, "chatgpt", vault.OAuthCredential{AccessToken: "fixture"}, []string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	retained, err := bot.Load(root, "before")
	if err != nil {
		t.Fatal(err)
	}
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, b, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	k := &Kernel{Bot: retained, Inbox: resources.box, Resources: resources, WorkDir: oldWork, SharedDir: shared, SkillsDir: skill.Dir(root), Memory: memory.Open(root)}
	save := k.resolveImageGen(ctx).gen.Save
	if save == nil {
		t.Fatal("image save fixture not enabled")
	}
	channel := k.reminders()
	if err := channel.Record(1, "fixture", "before-note", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := k.RecordExperience("before-experience", "", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := k.writeJob(compactJob{From: 1, To: 1, Status: jobRunning}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(bot.Dir(root, "before"), "memory"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bot.Dir(root, "before"), "memory", "reference.md"), []byte("---\ndescription: route-reference\nkeywords: [marker]\n---\nreference-body"), 0600); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Error("unexpected provider path", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(audioWAV())
	}))
	defer backend.Close()
	defer releaseOnce.Do(func() { close(release) })
	k.Provider = &provider.Client{BaseURL: backend.URL + "/v1", APIKey: "fixture", Model: "fixture", HTTPClient: backend.Client()}
	speech := make(chan error, 1)
	go func() { _, err := (ttsTool{k}).Run(ctx, json.RawMessage(`{"text":"fixture"}`)); speech <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("speech never reached the scripted provider")
	}
	moved := make(chan error, 1)
	go func() { moved <- resources.RenameTo("after") }()
	select {
	case err := <-moved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider wait held the filesystem route")
	}
	if k.Bot.Name() != "before" || k.currentName() != "after" {
		t.Fatal("fixture did not retain the stale handle")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-speech:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("speech did not finish")
	}
	_, ext := work.AudioType(audioWAV())
	voice := work.VoiceID(audioWAV(), ext)
	if data, err := k.readAudio(ctx, voice); err != nil || !bytes.Equal(data, audioWAV()) {
		t.Fatal("voice did not follow rename", err)
	}
	image := onePixelPNG(t)
	rel, err := save(image)
	if err != nil {
		t.Fatal("captured image save", err)
	}
	if data, err := k.readWorkspaceFile(ctx, filepath.Join(work.BotDir(root, "after"), filepath.FromSlash(rel)), work.PastedMaxBytes, nil); err != nil || !bytes.Equal(data, image) {
		t.Fatal("generated image did not follow rename", err)
	}
	k.turn = &turn{}
	ids, err := k.attachOutImages(ctx, []string{filepath.Join(work.BotDir(root, "after"), filepath.FromSlash(rel))}, []string{voice})
	if err != nil || len(ids) != 1 {
		t.Fatal("outgoing attachments", ids, err)
	}
	if _, data, err := k.readPasted(ids[0]); err != nil || !bytes.Equal(data, image) {
		t.Fatal("pasted image", err)
	}
	note := k.withLineNote(provider.RoleUser, "fixture", ids, []string{voice})
	if !strings.Contains(note, work.BotDir(root, "after")) || strings.Contains(note, oldWork) {
		t.Fatal("attachment display retained the old workspace", note)
	}
	if text, err := k.RecallHistory("kernel-path-marker"); err != nil || !strings.Contains(text, "kernel-path-marker") {
		t.Fatal("self history", text, err)
	}
	if text, err := k.Expand(1, 1); err != nil || !strings.Contains(text, "kernel-path-marker") {
		t.Fatal("self expansion", err)
	}
	entry, err := k.RecordExperience("after-experience", "", 1, 1)
	if err != nil || entry.ID != 2 {
		t.Fatal("experience append", entry, err)
	}
	if text, err := k.Detail(0, entry.ID); err != nil || !strings.Contains(text, "kernel-path-marker") {
		t.Fatal("experience detail", err)
	}
	if text, err := k.SearchExperiences("", 8); err != nil || !strings.Contains(text, "after-experience") {
		t.Fatal("experience read", err)
	}
	if err := channel.Record(1, "fixture", "after-note", time.Now()); err != nil {
		t.Fatal(err)
	}
	if notes, err := channel.log.all(); err != nil || len(notes) != 2 {
		t.Fatal("cached reminder log", notes, err)
	}
	if err := k.writeJob(compactJob{From: 1, To: 1, Status: jobReady, Summary: "after-summary"}); err != nil {
		t.Fatal(err)
	}
	if job, err := k.readJob(); err != nil || job.Summary != "after-summary" {
		t.Fatal("compact job", job, err)
	}
	unlock, err := k.lockJob(false)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if text, err := k.referenceText(); err != nil || !strings.Contains(text, "route-reference") || strings.Contains(text, bot.Dir(root, "before")) {
		t.Fatal("reference library", text, err)
	}
	if err := k.rememberBot(ctx, k.currentName(), "after-memory", "memory-route-marker"); err != nil {
		t.Fatal(err)
	}
	if text, err := (recallTool{k}).Run(ctx, json.RawMessage(`{"query":"memory-route-marker","scope":"bot"}`)); err != nil || !strings.Contains(text, "memory-route-marker") {
		t.Fatal("memory recall", text, err)
	}
	if _, _, own := k.memoryFolder(filepath.Join(bot.Dir(root, "after"), "memory", "memory.md")); !own {
		t.Fatal("memory guard lost self identity")
	}
	body := "fixture body"
	if _, err := k.SkillHandlers().Write(ctx, cliops.SkillWrite{Slug: "route-fixture", Description: "route fixture", Body: &body, Scope: "self"}); err != nil {
		t.Fatal("scoped skill write", err)
	}
	if _, err := k.SkillHandlers().Read(ctx, "route-fixture"); err != nil {
		t.Fatal("current skill switch", err)
	}
	if reply, err := k.BotHandlers().List(ctx); err != nil || reply.Local == nil || len(*reply.Local) != 1 || (*reply.Local)[0].Name != "other" {
		t.Fatal("peer identity", reply, err)
	}
	if reply, err := k.BotHandlers().Pin(ctx, cliops.BotPin{Name: "after", Pinned: true}); err != nil || !reply.Applied || reply.Name != "after" {
		t.Fatal("primary/self identity", err)
	}
	for _, path := range []string{bot.Dir(root, "before"), oldWork} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("retained Kernel revived its old directory", path, err)
		}
	}
}
