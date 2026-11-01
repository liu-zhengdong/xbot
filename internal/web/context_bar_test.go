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
 const document={createTextNode:t=>t};const state={sel:{type:'bot'},tab:'details',ctxSnap:null};let navigated=[];const selectWorkspaceTab=id=>{navigated.push(id);state.tab=id};
 ` + block("  const CTX_BANDS =", "  function ctxReq(") + block("  function paintContextBar(", "  function modelHits(") + `
 assert.equal(ctxK(27000),'27K');assert.equal(ctxK(128000),'128K');assert.equal(ctxK(1250),'1.3K');
 paintContextBar();assert.equal(host.hidden,true);assert.equal(host.children.length,0);
 state.ctxSnap={ceiling:100,target:50,requests:[{prompt:10},{prompt:30,tools:5,layers:[{key:'hist',tokens:20},{key:'turn',tokens:5}]}]};
 paintContextBar();assert.equal(host.hidden,false);const bar=host.children[0];assert.equal(bar.children[1].children[0].text,'30%');assert.equal(bar.children[3].text,'30 / 100');
 const band=bar.children[2];assert.deepEqual(band.children.slice(0,3).map(n=>n.cls),['ctx-c-turn','ctx-c-hist','ctx-c-tools']);assert.equal(band.children[2].style.marginLeft,'auto');assert.equal(band.children[3].style.left,'50%');
 bar.click();assert.equal(state.tab,'context');assert.deepEqual(navigated,['context']);paintContextBar();assert.equal(host.hidden,true);
 state.tab='details';state.sel.type='group';paintContextBar();assert.equal(host.hidden,true);
 state.sel.type='bot';state.ctxSnap.requests=[];paintContextBar();assert.equal(host.hidden,true);
 state.ctxSnap={ceiling:0,requests:[{prompt:30}]};paintContextBar();assert.equal(host.hidden,true);
 `
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// Both indirect entry points must reveal a hidden target and navigate through
// the same public workspace view as a direct tab click.
func TestContextAndTaskIndirectNavigationBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+`
const indirectFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).endsWith('/context'))return json({ceiling:100000,target:50000,requests:[{prompt:30000,tools:5000,layers:[{key:'hist',tokens:20000},{key:'turn',tokens:5000}]}]});
 const res=await indirectFetch(path,opts);
 if(/^\/api\/bots\/[^/]+$/.test(String(path))&&(!opts.method||opts.method==='GET'))return json({...await res.json(),agents:[{id:'sa_indirect00000001',type:'dev',task:'间接入口任务',status:'running',current_step:'检查入口',started_at:new Date().toISOString()}]});
 return res;
};
`, `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const tab=name=>[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent===name);
const hide=name=>{
 $('#workspace-more').click();
 const item=[...document.querySelectorAll('#workspace-more-menu button')].find(n=>n.textContent===name);
 assert(item?.checkVisibility(),'visibility option unreachable');item.click();
 document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));assert(!tab(name),'tab did not hide');
};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#ctxbar button')&&$('#work-status button')&&!$('.thread-loading-cover'),'indirect controls missing');
 hide('上下文');
 $('#ctxbar button').click();
 await waitFor(()=>tab('上下文')?.getAttribute('aria-selected')==='true'&&$('#panel').checkVisibility(),'context navigation failed to reveal its hidden tab');
 assert(!$('#thread').checkVisibility()&&!$('#ctxbar').checkVisibility(),'context left chat or context bar visible');
 tab('聊天').click();hide('子代理');
 $('#work-status button').click();
 await waitFor(()=>tab('子代理')?.getAttribute('aria-selected')==='true'&&document.getElementById(tab('子代理').getAttribute('aria-controls'))?.checkVisibility(),'busy navigation failed to reveal its hidden child tab');
 assert(document.getElementById(tab('子代理').getAttribute('aria-controls')).innerText.includes('间接入口任务'),'child destination lost running record');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>document.body.dataset.testResult='failed: '+e.message);
`)
}
