package web

import (
	"os/exec"
	"strings"
	"testing"
)

func TestAskOnlyMessageDoesNotLeaveEmptyRow(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function renderMessages(")
	b := strings.Index(s, "  function renderGroup(")
	script := `const assert=require('node:assert/strict');
 const el=(tag,cls)=>({tag,cls,children:[],append(...c){this.children.push(...c)}});
 const thread={scrollTop:0,scrollHeight:0,clientHeight:900,replaceChildren(inner){this.inner=inner}};
 const state={firstRender:true};const backgroundRelays=()=>new Set(),codingCardOf=()=>null,approvalCardOf=()=>null,skipMessage=()=>false,routineOf=()=>null,groupStart=()=>true,addTimeDividers=()=>{},restoreSendFeedback=()=>{};
 const botBubble=()=>el('div','msg bot');const toolSummary=()=>({name:'ask_user'});
 ` + s[strings.Index(s, "  const SAY_TOOLS"):strings.Index(s, "  // botBubble draws one bubble")] + s[strings.Index(s, "  const routineCache"):strings.Index(s, "  function taskAvatar(")] + s[a:b] + `
 renderMessages([{role:'assistant',content:'',tool_calls:[{function:{name:'ask_user'}}]}],'ada');
 assert.equal(thread.inner.children.length,0,'ask-only assistant must not append an empty hoverable message row');
 renderMessages([{role:'assistant',content:'先做哪件事？'}],'ada');assert.equal(thread.inner.children.length,1);
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestMissedRoutineShowsHumanExplanation(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function missedTimes(")
	if a < 0 {
		t.Fatal("missing missed-time formatter")
	}
	end := strings.Index(s[a:], "\n  // Drawers the owner")
	script := `const assert=require('node:assert/strict');const unwrap=text=>({untrusted:true,source:'timer',text});const when=()=>'';` + s[a:a+end] + `
 const text='Missed routine times from yesterday to today:\n- "secret-id" "检查测试": 2 missed (08:15, 08:30)\nThe computer was off or asleep. Decide whether anything is still worth doing now; reply with exactly SILENT.';
 const result=routineOf({role:'user',content:text});
 assert.ok(result.prompt.includes('错过 2 次（08:15、08:30）'));
 const now=new Date(2026,9,21,17);
 assert.equal(missedTimes('2026-10-21 08:15, 2026-10-21 08:30',now),'今天 08:15、08:30');
 assert.equal(missedTimes('2026-10-19 23:55, 2026-10-20 08:15, 2026-10-20 08:30, 2026-10-21 08:00, …',now),'10-19 23:55、昨天 08:15、08:30、今天 08:00、…');
 assert.equal(missedTimes('2025-10-21 08:15',now),'10-21 08:15');
 assert.equal(result.label,'错过的定时 · 1 项');assert.ok(result.prompt.includes('检查测试 · 错过 2 次'));
 for(const raw of ['secret-id','SILENT','Decide whether','Missed routine times']) assert.ok(!result.prompt.includes(raw),raw);
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestGatewayTextSpacing(t *testing.T) {
	raw, err := staticFS.ReadFile("static/devices.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  const sp =")
	b := strings.Index(s[a:], "\n")
	script := s[a:a+b] + `
 const assert=require('node:assert/strict');
 assert.equal(sp('连不上网关'+'xbot.babelgo.cn'),'连不上网关 xbot.babelgo.cn');
 assert.equal(sp('连不上网关'),'连不上网关');
 assert.equal(sp('等了3分钟，XBot已重连'),'等了 3 分钟，XBot 已重连');
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
