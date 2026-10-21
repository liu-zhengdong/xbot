package web

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRoutineRunsHideOnlyTheirSilentCompletion(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function routineRuns(")
	b := strings.Index(s, "  function taskAvatar(")
	script := `const assert=require('node:assert/strict');
 const routineOf=m=>m.routine?{label:m.routine}:null,unwrap=text=>({untrusted:text.startsWith('event:')});
 ` + s[a:b] + `
 const rows=[{seq:1,role:'user',routine:'新闻',content:'event:timer',branch:'wake'},
 {seq:2,role:'assistant',content:'另一个回合的回答'},
 {seq:3,role:'assistant',content:'SILENT',branch:'wake'},
 {seq:4,role:'user',routine:'提醒',content:'event:timer'},
 {seq:5,role:'assistant',content:'现在该出发了'}];
 const runs=routineRuns(rows);assert.deepEqual([...runs.hidden],[0,2]);assert.equal(runs.recent.length,2);assert.equal(runs.recent[0].summary,'静默完成');
 assert.equal(runs.recent[1].summary,'现在该出发了');
 const unfinished=routineRuns(rows.slice(0,1));assert.equal(unfinished.hidden.size,0);assert.equal(unfinished.recent.length,0);
 const owner=routineRuns([{role:'user',routine:'提醒',content:'event:timer'},{role:'user',content:'我的新问题'},{role:'assistant',content:'SILENT'}]);assert.equal(owner.hidden.size,0);
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestSlowToolNoticeRequiresAnOwnerInterjection(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function elapsedWork(")
	b := strings.Index(s, "  function routineRuns(")
	c := strings.Index(s, "  function slowToolNotice(")
	d := strings.Index(s, "  function updateWorkTimes(")
	script := `const assert=require('node:assert/strict'),unwrap=text=>({untrusted:text.startsWith('event:')});
 ` + s[a:b] + s[c:d] + `
 const current={tool:'shell',since:'2026-10-21T08:00:00Z'},now=Date.parse('2026-10-21T08:00:06Z');
 const initial={role:'user',content:'检查项目',time:'2026-10-21T07:59:00Z'};
 assert.equal(slowToolNotice(current,[initial],[],now),'');
 assert.equal(slowToolNotice(current,[{...initial,content:'event:timer',time:'2026-10-21T08:00:05Z'}],[],now),'');
 const aside={...initial,content:'顺便问一句',time:'2026-10-21T08:00:04Z'};
 assert.match(slowToolNotice(current,[initial,aside],[],now),/已 6 秒/);
 assert.match(slowToolNotice(current,[initial],[{dataset:{time:aside.time}}],now),/你的话会接在它后面/);
 assert.equal(slowToolNotice(current,[aside],[],Date.parse('2026-10-21T08:00:01Z')),'');
 assert.equal(slowToolNotice({},[aside],[],now),'');
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
