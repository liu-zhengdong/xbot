package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/cliops"
	"xbot/internal/tools"
)

func TestBotRuntimeCLIEarlyDispatchAndHelp(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	for _, args := range [][]string{{"list", "--help"}, {"info", "--help"}, {"new", "--persona-file", filepath.Join(absent, "missing"), "--help"}, {"delete", "--help"}} {
		output := captureStdout(t, func() error { return run(append([]string{"--root", absent}, args...)) })
		if !strings.Contains(output, "--self") || !strings.Contains(output, "offline") {
			t.Fatalf("incomplete help: %s", output)
		}
	}
	for _, args := range [][]string{{"list", "--self", "--json"}, {"info"}, {"new", "ada", "--persona-file", filepath.Join(absent, "missing")}, {"delete", "ada", "--json"}, {"list", "--persona", "--self"}} {
		if err := run(append([]string{"--root", absent}, args...)); err == nil {
			t.Errorf("runtime syntax reached offline path: %q", args)
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("help/error initialized root: %v", err)
	}
	t.Setenv("XBOT_ROOT", t.TempDir())
	if err := run([]string{"--root=", "list", "--self"}); err == nil || !strings.Contains(err.Error(), "explicit --root") {
		t.Fatalf("explicit empty root error=%v", err)
	}
}

func TestBotRuntimeCLIRequestsPartialAndRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_BOT", "stale-diagnostic-name")
	t.Setenv("XBOT_CALL_ID", "actual-source-call")
	calls := 0
	local, remote := []cliops.BotListLocal{}, []cliops.BotListRemote{}
	bots := []cliops.BotStatusView{{Bot: "ada", State: "idle"}}
	persona := " \n" + strings.Repeat("p", 33<<10) + "\n "
	source, err := cliops.Start(root, cliops.Handlers{Bots: &cliops.BotHandlers{
		List: func(ctx context.Context) (cliops.BotListReply, error) {
			calls++
			if cliops.SourceCallID(ctx) != "" {
				t.Errorf("list inherited call ID")
			}
			return cliops.BotListReply{Local: &local, Remote: &remote}, nil
		},
		Info: func(ctx context.Context, query cliops.BotInfoQuery) (cliops.BotInfoReply, error) {
			calls++
			if query.Bot != "ada" || query.Steps != 99 || cliops.SourceCallID(ctx) != "" {
				t.Errorf("info=%+v call=%q", query, cliops.SourceCallID(ctx))
			}
			return cliops.BotInfoReply{Bots: &bots, Count: 1}, nil
		},
		Create: func(ctx context.Context, input cliops.BotCreate) (cliops.BotCreateReply, error) {
			calls++
			if input.Name != "ada" || input.Persona != persona || cliops.SourceCallID(ctx) != "" {
				t.Errorf("create name=%q persona bytes=%d call=%q", input.Name, len(input.Persona), cliops.SourceCallID(ctx))
			}
			return cliops.BotCreateReply{Name: input.Name, Applied: true, Stage: "persona"}, errors.New("persona exceeds 32 KiB")
		},
		Delete: func(ctx context.Context, name string) (cliops.BotDeleteReply, error) {
			calls++
			if name != "ada" || cliops.SourceCallID(ctx) != "actual-source-call" {
				t.Errorf("delete=%q call=%q", name, cliops.SourceCallID(ctx))
			}
			return cliops.BotDeleteReply{Name: name, Removed: true, Stage: "vault_key"}, errors.New("vault key cleanup failed")
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	var out bytes.Buffer
	ctx := context.Background()
	if err := runBotRuntime(ctx, root, true, []string{"list", "--self", "--json"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"local\":[],\"remote\":[],\"count\":0}\n" {
		t.Fatalf("empty list stdout=%q", out.String())
	}
	out.Reset()
	if err := runBotRuntime(ctx, "", false, []string{"info", "ada", "--steps", "99", "--self"}, nil, &out); err != nil || out.String() != cliops.FormatBotInfo(cliops.BotInfoReply{Bots: &bots, Count: 1}) {
		t.Fatalf("info stdout=%q error=%v", out.String(), err)
	}
	out.Reset()
	err = runBotRuntime(ctx, "", false, []string{"new", "ada", "--persona-file", "-", "--self", "--json"}, strings.NewReader(persona), &out)
	var created cliops.BotCreateReply
	if decode := json.Unmarshal(out.Bytes(), &created); decode != nil || err == nil || !created.Applied || created.Stage != "persona" || created.Error != "persona exceeds 32 KiB" {
		t.Fatalf("create stdout=%s error=%v decode=%v", out.String(), err, decode)
	}
	out.Reset()
	err = runBotRuntime(ctx, "", false, []string{"delete", "ada", "--self"}, nil, &out)
	if err == nil || out.String() != "name: ada\nremoved: true\nstage: vault_key\nerror: vault key cleanup failed\n" {
		t.Fatalf("delete stdout=%q error=%v", out.String(), err)
	}
	if calls != 4 {
		t.Fatalf("calls=%d want four without retry", calls)
	}
	out.Reset()
	if err := runBotRuntime(ctx, t.TempDir(), true, []string{"list", "--self"}, nil, &out); err == nil || out.Len() != 0 || calls != 4 {
		t.Fatalf("wrong root calls=%d stdout=%q error=%v", calls, out.String(), err)
	}
}

func TestBotRuntimeCLIArtifactsAndBothOutputBudgets(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	var body []byte
	requests := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; _, _ = w.Write(body) }))
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.URL)
	ctx := context.Background()
	var out bytes.Buffer
	artifact := cliops.BotListReply{Count: 50, File: "results/bots.json", Access: "local", Bytes: 123456}
	body, _ = json.Marshal(artifact)
	if err := runBotRuntime(ctx, "", false, []string{"list", "--self"}, nil, &out); err != nil || out.String() != cliops.FormatBotList(artifact) {
		t.Fatalf("artifact stdout=%q error=%v", out.String(), err)
	}
	quote := strings.Repeat(`"`, 17000)
	local := []cliops.BotListLocal{{Name: quote}}
	bots := []cliops.BotStatusView{{Bot: quote, State: "idle"}}
	for _, tc := range []struct {
		args  []string
		reply any
		text  string
	}{
		{[]string{"list", "--self"}, cliops.BotListReply{Local: &local, Count: 1}, cliops.FormatBotList(cliops.BotListReply{Local: &local, Count: 1})},
		{[]string{"info", "--self"}, cliops.BotInfoReply{Bots: &bots, Count: 1}, cliops.FormatBotInfo(cliops.BotInfoReply{Bots: &bots, Count: 1})},
		{[]string{"new", "ada", "--self", "--persona=p"}, cliops.BotCreateReply{Name: quote, Applied: true, Stage: "complete"}, cliops.FormatBotCreate(cliops.BotCreateReply{Name: quote, Applied: true, Stage: "complete"})},
		{[]string{"delete", "ada", "--self"}, cliops.BotDeleteReply{Name: quote, Removed: true, Stage: "complete"}, cliops.FormatBotDelete(cliops.BotDeleteReply{Name: quote, Removed: true, Stage: "complete"})},
	} {
		body, _ = json.Marshal(tc.reply)
		if len(body)+1 <= tools.DefaultShellOutputBytes || len(body) > cliops.BotMaxReply || len(tc.text) > tools.DefaultShellOutputBytes {
			t.Fatalf("invalid JSON-only overflow fixture: json=%d human=%d", len(body)+1, len(tc.text))
		}
		for _, asJSON := range []bool{false, true} {
			args := append([]string{}, tc.args...)
			if asJSON {
				args = append(args, "--json")
			}
			out.Reset()
			if err := runBotRuntime(ctx, "", false, args, nil, &out); err == nil || out.Len() != 0 {
				t.Fatalf("JSON overflow %q stdout bytes=%d error=%v", args, out.Len(), err)
			}
		}
	}
	verbose := make([]cliops.BotStatusView, 300)
	for i := range verbose {
		verbose[i] = cliops.BotStatusView{Bot: "b", State: "idle", Tokens: &cliops.BotTokenUse{}}
	}
	info := cliops.BotInfoReply{Bots: &verbose, Count: len(verbose)}
	body, _ = json.Marshal(info)
	if len(body)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatBotInfo(info)) <= tools.DefaultShellOutputBytes {
		t.Fatalf("invalid human-only overflow fixture: json=%d human=%d", len(body)+1, len(cliops.FormatBotInfo(info)))
	}
	for _, args := range [][]string{{"info", "--self"}, {"info", "--self", "--json"}} {
		out.Reset()
		if err := runBotRuntime(ctx, "", false, args, nil, &out); err == nil || out.Len() != 0 {
			t.Fatalf("human overflow %q stdout bytes=%d error=%v", args, out.Len(), err)
		}
	}
	if requests != 11 {
		t.Fatalf("requests=%d want 11 without retry", requests)
	}
}
