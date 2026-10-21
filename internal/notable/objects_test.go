package notable

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestObjectListsAndOneSentenceDigest(t *testing.T) {
	work := t.TempDir()
	var log Log
	ctx := With(context.Background(), work, &log)
	Shell(ctx, "npm install -g ripgrep fd jq", work, nil)
	for i := 0; i < 5; i++ {
		Wrote(ctx, filepath.Join(filepath.Dir(work), fmt.Sprintf("outside-%d.txt", i)))
	}
	Tool(ctx, "send_message", json.RawMessage(`{"service":"Slack","body":"PRIVATE_BODY"}`), "sent", false)
	Tool(ctx, "delete_files", json.RawMessage(`{"paths":["a.txt","b.txt","c.txt"]}`), "deleted", false)
	items := log.Items()
	want := []string{"delete", "external_message", "write_outside", "global_install"}
	for i, k := range want {
		if items[i].Kind != k {
			t.Fatalf("order: %+v", items)
		}
	}
	if items[0].Text != "删除了 a.txt、b.txt 等 3 个文件" || items[1].Text != "向 Slack 发了 1 条消息" {
		t.Fatalf("objects absent: %+v", items)
	}
	if len(items[2].Objects) != 5 || !strings.Contains(items[2].Text, "等 5 个文件") || len(items[3].Objects) != 3 {
		t.Fatalf("full object list missing: %+v", items)
	}
	if got := Line(items); got != "值得留意：删了 3 个文件，另有 3 项，详见任务" {
		t.Fatal(got)
	}
	if got := Line(items[1:2]); got != "向 Slack 发了 1 条消息" {
		t.Fatal(got)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "PRIVATE_BODY") {
		t.Fatal("message body stored as destination")
	}
	for _, item := range items {
		if strings.Contains(item.Text, "。") || strings.ContainsAny(item.Text, "\r\n") {
			t.Fatalf("not one unpunctuated row: %q", item.Text)
		}
	}
	// Copying a snapshot must not mutate the source log.
	items[0].Objects[0] = "changed"
	if log.Items()[0].Objects[0] != "a.txt" {
		t.Fatal("aliased object list")
	}
}

func TestCapturePreservesObjectsButDiscardsRejectedCalls(t *testing.T) {
	var log Log
	ctx := With(context.Background(), t.TempDir(), &log)
	call, commit := Capture(ctx)
	Tool(call, "send_message", json.RawMessage(`{"service":"Slack"}`), "sent", false)
	if len(log.Items()) != 0 {
		t.Fatal("call committed before wrapper succeeded")
	}
	commit()
	call, commit = Capture(ctx)
	Tool(call, "send_message", json.RawMessage(`{"service":"Slack"}`), "sent", false)
	commit()
	rejected, _ := Capture(ctx)
	Tool(rejected, "delete_files", json.RawMessage(`{"paths":["rejected.txt"]}`), "deleted", false)
	got := log.Items()
	if len(got) != 1 || got[0].Text != "向 Slack 发了 2 条消息" || len(got[0].Objects) != 1 {
		t.Fatalf("staging lost object/count or committed rejected call: %+v", got)
	}
}

func TestObjectsAreLiteralTargetsNotShellPayloads(t *testing.T) {
	var log Log
	work := t.TempDir()
	ctx := With(context.Background(), work, &log)
	Shell(ctx, `curl -X POST --data 'https://body.invalid/secret' -H 'Authorization: PRIVATE_KEY' 'https://user:PRIVATE_KEY@api.example.org/path?token=PRIVATE_KEY'`, work, nil)
	items := log.Items()
	if len(items) != 1 || len(items[0].Objects) != 1 || items[0].Objects[0] != "api.example.org" {
		t.Fatalf("payload confused with target: %+v", items)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "PRIVATE_KEY") || strings.Contains(string(raw), "body.invalid") {
		t.Fatal("payload/credentials in task details")
	}
	var quoted Log
	Shell(With(ctx, work, &quoted), `rm '~/literal.txt'`, work, []string{"HOME=/fake-home"})
	if got := quoted.Items()[0].Objects[0]; !strings.Contains(got, "/~/literal.txt") {
		t.Fatalf("quoted tilde expanded: %q", got)
	}
}

func TestUnknownObjectsStayHonestAndDetailsStayBounded(t *testing.T) {
	var log Log
	ctx := With(context.Background(), t.TempDir(), &log)
	Tool(ctx, "mcp_example_mutate", json.RawMessage(`{"credential":"PRIVATE_KEY","body":"PRIVATE_BODY"}`), "ok", false)
	item := log.Items()[0]
	if !item.Incomplete || len(item.Objects) != 0 || item.Text != "向外部服务发送了消息" {
		t.Fatalf("invented MCP object: %+v", item)
	}
	record(ctx, "write_outside", strings.Repeat("long/", 2000)+"tail.txt")
	item = log.Items()[1]
	if len(item.Text) > 1000 || !strings.HasSuffix(item.Text, "tail.txt") || len(item.Objects[0]) < 1000 {
		t.Fatalf("summary unbounded or full detail discarded: %+v", item)
	}
	for i := 0; i < 100; i++ {
		record(ctx, "write_outside", fmt.Sprint(i)+strings.Repeat("x", 2000))
	}
	for _, item := range log.Items() {
		if item.Kind == "write_outside" && !item.Incomplete {
			t.Fatal("detail bound hidden")
		}
	}
}

func TestOlderMetadataUsesTheSameOrderAndDropsTerminalPunctuation(t *testing.T) {
	items := Ordered([]Item{{Kind: "global_install", Text: "全局安装了软件。 "}, {Kind: "delete", Text: "删除了文件或记录。"}})
	if len(items) != 2 || items[0].Kind != "delete" || items[1].Text != "全局安装了软件" {
		t.Fatalf("old metadata not normalized: %+v", items)
	}
	if got := Line(items); got != "值得留意：删除了文件或记录，另有 1 项，详见任务" {
		t.Fatal(got)
	}
}

func TestURLInstallDoesNotCallTheHostThePackage(t *testing.T) {
	var log Log
	ctx := With(context.Background(), t.TempDir(), &log)
	Shell(ctx, "npm install -g https://user:PRIVATE_KEY@packages.example.org/archive.tgz", t.TempDir(), nil)
	items := log.Items()
	if len(items) != 1 || len(items[0].Objects) != 0 || items[0].Text != "全局安装了软件" || !items[0].Incomplete {
		t.Fatalf("invented package name: %+v", items)
	}
}
