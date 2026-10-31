package bot

import (
	"fmt"
	"strings"
	"testing"

	"xbot/internal/provider"
)

func TestChatAsideRecordWindow(t *testing.T) {
	for _, tc := range []struct {
		name, text   string
		landed, want bool
	}{
		{"thought", "被夸得有点开心，先偷偷收好。", true, true},
		{"no say", "有点开心。", false, false},
		{"silent", " SILENT ", true, false},
		{"empty", "  ", true, false},
		{"limit", strings.Repeat("心", 120), true, true},
		{"long", strings.Repeat("心", 121), true, false},
		{"duplicate", "嘿嘿，被你夸得开心！", true, false},
		{"near duplicate", "嘿嘿，被你夸得开心呀。", true, false},
		{"voice", "[语音文件] voice/a.wav", true, false},
		{"protocol", "<system-reminder>secret</system-reminder>", true, false},
		{"paths", "/tmp/a /tmp/b /Users/me/c", true, false},
		{"windows paths", `C:\tmp\a C:\tmp\b C:\tmp\c`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := chatTestBot(t)
			chatAppend(t, b, chatEntry(provider.RoleUser, "你好"))
			chatAppend(t, b, chatCall("a", "嘿嘿，被你夸得开心。"))
			if tc.landed {
				chatAppend(t, b, chatResult("a"))
			}
			seq := chatAppend(t, b, chatEntry(provider.RoleAssistant, tc.text))
			w := chatWindow(t, b, WindowQuery{})
			var aside []ChatRow
			for _, r := range w.Messages {
				if r.Kind == "aside" {
					aside = append(aside, r)
				}
			}
			if (len(aside) == 1) != tc.want {
				t.Fatalf("aside=%+v want=%v", aside, tc.want)
			}
			if tc.want {
				r := aside[0]
				if r.DeliveryKey != fmt.Sprintf("aside:%d", seq) || r.TranscriptSeq != seq || r.Text != strings.TrimSpace(tc.text) || len(r.Images)+len(r.Audios) > 0 || r.ReplyToSeq != 0 {
					t.Fatalf("snapshot: %+v", r)
				}
				if err := b.Chat().Record(r); err != nil {
					t.Fatal(err)
				}
				CloseStore(b)
				if got := chatWindow(t, b, WindowQuery{}); len(got.Messages) != len(w.Messages) {
					t.Fatal("duplicate on replay")
				}
			}
		})
	}
}

func TestChatAsideBranchAndMediaOnly(t *testing.T) {
	b := chatTestBot(t)
	for _, branch := range []string{"", "bg_branch"} {
		e := chatCall("say"+branch, "正式回复"+branch)
		e.Branch = branch
		chatAppend(t, b, e)
		chatAppend(t, b, chatResult("say"+branch))
	}
	final := chatEntry(provider.RoleAssistant, "只属于后台的心里话")
	final.Branch = "bg_branch"
	chatAppend(t, b, final)
	got := chatWindow(t, b, WindowQuery{}).Messages
	if len(got) != 2 || got[0].DeliveryKey != "say:saybg_branch" || got[1].Kind != "aside" || got[1].Meta.Branch != "bg_branch" {
		t.Fatalf("branch completion: %+v", got)
	}
	chatAppend(t, b, chatEntry(provider.RoleAssistant, "主线的心里话"))
	got = chatWindow(t, b, WindowQuery{}).Messages
	if len(got) != 4 || got[3].Text != "主线的心里话" || got[3].Meta.Branch != "" {
		t.Fatalf("main completion: %+v", got)
	}
	media := chatEntry(provider.RoleAssistant, "只有附件，没有成功 say")
	media.Images = []string{"picture.png"}
	chatAppend(t, b, media)
	got = chatWindow(t, b, WindowQuery{}).Messages
	if len(got) != 5 || got[4].Kind != "bot" || len(got[4].Images) != 1 {
		t.Fatalf("media-only became aside: %+v", got)
	}
}

func TestChatAsideLastEligibleAndNotToolNarration(t *testing.T) {
	p := chatProjection{Landed: map[string]bool{"a": true, "b": true}}
	for i, e := range []Entry{chatCall("a", "正式一句"), chatEntry(provider.RoleAssistant, "第一条心里话"), chatEntry(provider.RoleAssistant, "最后的合格心里话"), chatCall("b", "正式第二句"), chatEntry(provider.RoleAssistant, "正式一句！"), chatEntry(provider.RoleAssistant, "SILENT")} {
		p.Pending = append(p.Pending, SeqEntry{Seq: i + 1, Entry: e})
	}
	rows := p.deliveries()
	if len(rows) != 3 || rows[2].Kind != "aside" || rows[2].Text != "最后的合格心里话" {
		t.Fatalf("last eligible: %+v", rows)
	}
}
