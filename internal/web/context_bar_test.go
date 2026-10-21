package web

import (
	"os/exec"
	"strings"
	"testing"
)

func TestContextBarUsesLatestSnapshot(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	block := func(a, b string) string {
		t.Helper()
		start := strings.Index(s, a)
		if start < 0 {
			t.Fatalf("missing %s", a)
		}
		end := strings.Index(s[start:], b)
		if end < 0 {
			t.Fatalf("missing %s", b)
		}
		return s[start : start+end]
	}
	script := `const assert=require('node:assert/strict');
 const node=(tag,cls,text)=>({tag,cls,text,children:[],style:{},hidden:false,append(...c){this.children.push(...c)},replaceChildren(){this.children=[]},setAttribute(){},addEventListener(_,f){this.click=f}});
 const host=node('div');const $=()=>host,el=node,icon=()=>node('svg'),setTip=()=>{};
 const document={createTextNode:t=>t};const state={sel:{type:'bot'},tab:'details',ctxSnap:null};let paints=0;const paintPanel=()=>paints++;
 ` + block("  const CTX_BANDS =", "  function ctxReq(") + block("  function paintContextBar(", "  function styleChips(") + `
 paintContextBar();assert.equal(host.hidden,true);assert.equal(host.children.length,0);
 state.ctxSnap={ceiling:100,target:50,requests:[{prompt:10},{prompt:30,tools:5,layers:[{key:'hist',tokens:20},{key:'turn',tokens:5}]}]};
 paintContextBar();assert.equal(host.hidden,false);const bar=host.children[0];assert.equal(bar.children[1].children[0].text,'30%');assert.equal(bar.children[3].text,'30 / 100');
 const band=bar.children[2];assert.deepEqual(band.children.slice(0,3).map(n=>n.cls),['ctx-c-turn','ctx-c-hist','ctx-c-tools']);assert.equal(band.children[2].style.marginLeft,'auto');assert.equal(band.children[3].style.left,'50%');
 bar.click();assert.equal(state.tab,'context');assert.equal(paints,1);paintContextBar();assert.equal(host.hidden,true);
 state.tab='details';state.sel.type='group';paintContextBar();assert.equal(host.hidden,true);
 state.sel.type='bot';state.ctxSnap.requests=[];paintContextBar();assert.equal(host.hidden,true);
 state.ctxSnap={ceiling:0,requests:[{prompt:30}]};paintContextBar();assert.equal(host.hidden,true);
 `
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestUsageSnapshotRefreshesWhenThreadChanges(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  async function loadThread(")
	b := strings.Index(s, "  function section(")
	script := `const assert=require('node:assert/strict');
 const state={sel:{type:'bot',id:'ada'},threadLoad:0,sig:'',messages:[],preview:{},previewAt:{},pendingSends:[],tab:'details'};
 let messages=[],loads=0,open=true;const api=async()=>({messages});const panel={classList:{contains:()=>open}};
 const previewFromMessages=()=>'',renderLists=()=>{},renderMessages=()=>{},paintPanel=()=>{},threadUnavailable=()=>{throw Error('unexpected load failure')};
 const loadPanel=async()=>{loads++};
 ` + s[a:b] + `
 (async()=>{
 await loadThread();assert.equal(loads,1,'first transcript should load the snapshot');
 await loadThread();assert.equal(loads,1,'an unchanged poll must not refetch the full context');
 messages=[{seq:1,role:'assistant',content:'done'}];await loadThread();assert.equal(loads,2,'a new reply must refresh the usage bar');
 open=false;messages.push({seq:2,role:'assistant',content:'later'});await loadThread();assert.equal(loads,2,'closed panel needs no context request');
 })().catch(e=>{console.error(e);process.exit(1)});
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
