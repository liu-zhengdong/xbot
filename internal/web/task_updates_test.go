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

func TestFailedTaskKeepsItsOriginalErrorAcrossPolls(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	part := func(start, end string) string {
		a, b := strings.Index(s, start), strings.Index(s, end)
		if a < 0 || b <= a {
			t.Fatal("task rendering functions not found")
		}
		return s[a:b]
	}
	script := `const assert=require('node:assert/strict');
 const node=()=>({dataset:{},hidden:false,textContent:'',children:[],classList:{toggle(){},contains(){return true}},setAttribute(){},addEventListener(t,fn){this[t]=fn},append(...xs){this.children.push(...xs)},replaceChildren(...xs){this.children=xs},querySelector(sel){return this.children.find(x=>sel==='button'&&x.tag==='button')||null},get childElementCount(){return this.children.length},firstElementChild:{replaceWith(){}}});
 const el=(tag,cls,text)=>Object.assign(node(),{tag,textContent:text||''}), icon=()=>node(),spinIcon=icon;
 const taskStatus=x=>x,when=x=>x,clip=x=>x,plainText=x=>x,displayText=x=>x,elapsedWork=()=>'',zhError=()=> '请求超时';
 const elements=new Map([['.task-error',node()]]),card=node();card.querySelector=sel=>{if(!elements.has(sel))elements.set(sel,node());return elements.get(sel)};
 ` + part("  function errorDetails(", "  function visibleError(") + part("  function updateTaskCard(", "  const routineChanges") + `
 const ag={id:'w1',status:'failed',summary:'provider: context deadline exceeded',steps:3};
 updateTaskCard(card,ag);
 const detail=elements.get('.task-error');assert.equal(detail.hidden,false);assert.equal(detail.children.length,2);assert.equal(detail.children[1].textContent,ag.summary);
 detail.children[0].click();assert.equal(detail.children[1].hidden,false);
 const prior=detail.children[1];updateTaskCard(card,ag);assert.equal(detail.children[1],prior);assert.equal(prior.hidden,false);
 updateTaskCard(card,{...ag,status:'running'});assert.equal(detail.hidden,true);assert.equal(detail.children.length,0);
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
