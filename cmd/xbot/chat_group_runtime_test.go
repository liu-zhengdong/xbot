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
)

func TestChatGroupRuntimeEarlyHelpAndRoot(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	absent := filepath.Join(t.TempDir(), "absent")
	for _, args := range [][]string{{"--help"}, {"list", "--help"}, {"history", "--help"}, {"create", "--help"}, {"post", "--file", filepath.Join(absent, "missing"), "--help"}} {
		out := captureStdout(t, func() error { return run(append([]string{"--root", absent, "chat-group"}, args...)) })
		if !strings.Contains(out, "--self") || !strings.Contains(out, "history GROUP_ID") {
			t.Fatalf("help=%s", out)
		}
	}
	for _, args := range [][]string{{"list", "--self"}, {"history", "g"}, {"create", "n", "--self", "--member=B"}, {"post", "g", "--self", "--file=-"}} {
		if err := run(append([]string{"--root", absent, "chat-group"}, args...)); err == nil {
			t.Fatalf("runtime succeeded without source: %q", args)
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("root initialized: %v", err)
	}
	t.Setenv("XBOT_ROOT", t.TempDir())
	if err := run([]string{"--root=", "chat-group", "list", "--self"}); err == nil || !strings.Contains(err.Error(), "explicit --root") {
		t.Fatalf("empty root err=%v", err)
	}
	var out bytes.Buffer
	if err := runChatGroupRuntime(context.Background(), t.TempDir(), true, []string{"list", "--self"}, nil, &out); err == nil || !strings.Contains(err.Error(), "mismatch") || out.Len() != 0 {
		t.Fatalf("mismatch out=%q err=%v", out.String(), err)
	}
}

func TestChatGroupRuntimeCLIRequestsAndPartial(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_BOT", "ignored-stale")
	t.Setenv("XBOT_CALL_ID", "original")
	calls := 0
	groups := []cliops.ChatGroup{}
	lines := []cliops.ChatGroupLine{}
	body := " \n" + strings.Repeat("<&>\"", 2048) + "\n "
	source, err := cliops.Start(root, cliops.Handlers{ChatGroups: &cliops.ChatGroupHandlers{
		List: func(ctx context.Context) (cliops.ChatGroupListReply, error) {
			calls++
			if cliops.SourceCallID(ctx) != "" {
				t.Error("list inherited call ID")
			}
			return cliops.ChatGroupListReply{Groups: &groups}, nil
		},
		History: func(ctx context.Context, input cliops.ChatGroupHistory) (cliops.ChatGroupHistoryReply, error) {
			calls++
			if input.ID != "g" || input.Keyword != "--help" || input.Limit != 99 || cliops.SourceCallID(ctx) != "" {
				t.Errorf("history=%+v", input)
			}
			return cliops.ChatGroupHistoryReply{Messages: &lines, Untrusted: true}, nil
		},
		Create: func(ctx context.Context, input cliops.ChatGroupCreate) (cliops.ChatGroupCreateReply, error) {
			calls++
			if input.Name != "n" || len(input.Members) != 2 || input.Members[1] != "B" || cliops.SourceCallID(ctx) != "" {
				t.Errorf("create=%+v", input)
			}
			return cliops.ChatGroupCreateReply{Created: true}, nil
		},
		Post: func(ctx context.Context, input cliops.ChatGroupPost) (cliops.ChatGroupPostReply, error) {
			calls++
			if input.ID != "g" || input.Text != body || cliops.SourceCallID(ctx) != "original" {
				t.Errorf("post id=%q bytes=%d call=%q", input.ID, len(input.Text), cliops.SourceCallID(ctx))
			}
			return cliops.ChatGroupPostReply{ID: "g", MessageID: "gm", Appended: true, PostedMembers: []string{"B"}, FailedMember: "C", Stage: "recipient_open"}, errors.New("late failure")
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	var out bytes.Buffer
	ctx := context.Background()
	if err := runChatGroupRuntime(ctx, root, true, []string{"list", "--self", "--json"}, nil, &out); err != nil || out.String() != "{\"groups\":[],\"count\":0}\n" {
		t.Fatalf("list=%q err=%v", out.String(), err)
	}
	out.Reset()
	if err := runChatGroupRuntime(ctx, "", false, []string{"history", "g", "--self", "--keyword", "--help", "--limit=99"}, nil, &out); err != nil || out.String() != cliops.FormatChatGroupHistory(cliops.ChatGroupHistoryReply{Messages: &lines, Untrusted: true}) {
		t.Fatalf("history=%q err=%v", out.String(), err)
	}
	out.Reset()
	if err := runChatGroupRuntime(ctx, "", false, []string{"create", "n", "--self", "--member=B", "--member=B"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = runChatGroupRuntime(ctx, "", false, []string{"post", "g", "--self", "--file=-", "--json"}, strings.NewReader(body), &out)
	var partial cliops.ChatGroupPostReply
	if decode := json.Unmarshal(out.Bytes(), &partial); err == nil || decode != nil || !partial.Appended || partial.MessageID != "gm" || len(partial.PostedMembers) != 1 || partial.Stage != "recipient_open" || !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("partial=%s err=%v decode=%v", out.String(), err, decode)
	}
	if calls != 4 {
		t.Fatalf("calls=%d want 4 without retry", calls)
	}
}

func TestChatGroupRuntimeInputRawAndEscapedBudgets(t *testing.T) {
	text := " \n" + strings.Repeat("<&>\"", 2048) + "\n "
	path := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []cliops.ChatGroupCommand{{ID: "g", Text: text}, {ID: "g", TextFile: path}, {ID: "g", TextFile: "-"}} {
		input, err := readChatGroupPost(command, strings.NewReader(text))
		if err != nil || input.ID != "g" || input.Text != text {
			t.Fatalf("input bytes=%d err=%v", len(input.Text), err)
		}
	}
	for _, tc := range []struct {
		command cliops.ChatGroupCommand
		stdin   string
	}{
		{cliops.ChatGroupCommand{ID: "g", Text: strings.Repeat("x", cliops.ChatGroupMaxRequest+1)}, ""},
		{cliops.ChatGroupCommand{ID: "g", TextFile: "-"}, strings.Repeat("x", cliops.ChatGroupMaxRequest+1)},
		{cliops.ChatGroupCommand{ID: "g", Text: strings.Repeat("<", cliops.ChatGroupMaxRequest/6+1)}, ""},
		{cliops.ChatGroupCommand{ID: strings.Repeat("g", cliops.ChatGroupMaxRequest), Text: "x"}, ""},
	} {
		if _, err := readChatGroupPost(tc.command, strings.NewReader(tc.stdin)); err == nil {
			t.Fatal("oversized request accepted")
		}
	}
	base := `{"id":"g","text":""}`
	exact := strings.Repeat("x", cliops.ChatGroupMaxRequest-len(base))
	if _, err := readChatGroupPost(cliops.ChatGroupCommand{ID: "g", Text: exact}, nil); err != nil {
		t.Fatalf("exact limit: %v", err)
	}
	if _, err := readChatGroupPost(cliops.ChatGroupCommand{ID: "g", Text: exact + "x"}, nil); err == nil {
		t.Fatal("encoded max+1 accepted")
	}
}

func TestChatGroupRuntimeOutputBudgetsAndFacts(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	var body []byte
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.URL)
	var out bytes.Buffer
	ctx := context.Background()
	reference := cliops.ChatGroupListReply{Count: 50, File: "/work/results/groups.json", Access: "local", Bytes: 123456}
	body, _ = json.Marshal(reference)
	if err := runChatGroupRuntime(ctx, "", false, []string{"list", "--self"}, nil, &out); err != nil || out.String() != cliops.FormatChatGroupList(reference) {
		t.Fatalf("reference=%q err=%v", out.String(), err)
	}
	groups := []cliops.ChatGroup{{Name: strings.Repeat("x", 33<<10), Members: []string{}}}
	body, _ = json.Marshal(cliops.ChatGroupListReply{Groups: &groups, Count: 1})
	for _, args := range [][]string{{"list", "--self"}, {"list", "--self", "--json"}} {
		out.Reset()
		if err := runChatGroupRuntime(ctx, "", false, args, nil, &out); err == nil || out.Len() != 0 {
			t.Fatalf("overflow bytes=%d err=%v", out.Len(), err)
		}
	}
	body, _ = json.Marshal(cliops.ChatGroupPostReply{ID: "g", MessageID: "gm", Appended: true, PostedMembers: []string{"B"}, Stage: "recipient_open", Error: strings.Repeat("e", 40<<10)})
	out.Reset()
	err := runChatGroupRuntime(ctx, "", false, []string{"post", "g", "--self", "--text=x", "--json"}, nil, &out)
	var post cliops.ChatGroupPostReply
	if decode := json.Unmarshal(out.Bytes(), &post); err == nil || decode != nil || !post.Appended || post.MessageID != "gm" || len(post.PostedMembers) != 1 || out.Len() > 32<<10 {
		t.Fatalf("post=%s err=%v decode=%v", out.String(), err, decode)
	}
	body, _ = json.Marshal(cliops.ChatGroupCreateReply{Group: &cliops.ChatGroup{Name: strings.Repeat("x", 40<<10)}, Created: true})
	out.Reset()
	err = runChatGroupRuntime(ctx, "", false, []string{"create", "n", "--self", "--member=B", "--json"}, nil, &out)
	var created cliops.ChatGroupCreateReply
	if decode := json.Unmarshal(out.Bytes(), &created); err == nil || decode != nil || !created.Created || out.Len() > 32<<10 {
		t.Fatalf("created=%s err=%v decode=%v", out.String(), err, decode)
	}
}
