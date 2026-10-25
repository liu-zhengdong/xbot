package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A script that stops early (an awaited promise that never settles lets node exit 0) must fail, so each
// script sets finished at its last line and this hook turns a missing flag into a non-zero exit.
const finishGuard = "let finished=false;process.on('exit',()=>{if(!finished){console.error('script did not reach its end');process.exitCode=1}});\n"

// Behaviour of the settings page script against a fake DOM and a fake API: filtering, the same-name
// question before overwriting, and an on/off switch that falls back when saving fails.
func TestSkillsSettingsBehaviour(t *testing.T) {
	source, err := staticFS.ReadFile("static/skills.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(string(source))
	script := finishGuard + `const assert=require('node:assert/strict'),vm=require('node:vm');
 const all=[];
 class Node{constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.className='';this.textContent='';this.value='';this.hidden=false;all.push(this);
  this.classList={toggle:(n,on)=>{const s=new Set(this.className.split(' ').filter(Boolean));on?s.add(n):s.delete(n);this.className=[...s].join(' ')}}}
  append(...n){this.children.push(...n)} replaceChildren(...n){this.children=n} setAttribute(k,v){this.attrs[k]=v} focus(){document.activeElement=this} click(){this.clicked=(this.clicked||0)+1}}
 const window={},document={createElement:t=>new Node(t),createTextNode:t=>({textContent:t}),activeElement:null};
 vm.runInNewContext(` + string(encoded) + `,{window,document});
 const find=(cls,from=all)=>from.filter(n=>n.className.split(' ').includes(cls));
 const library=[{slug:'weekly',name:'写周报',description:'整理周报'},{slug:'flight',name:'订机票',description:'比较航班'}];
 (async()=>{
  const calls=[];let saveFails=false,exists=true;
  const api=async(path,opt)=>{calls.push([opt?.method||'GET',path,opt?.body]);
   if(path==='/api/skills')return {skills:structuredClone(library)};
   if(path==='/api/skills/weekly')return {slug:'weekly',name:'写周报',description:'整理周报',bots:[{name:'ada',enabled:true},{name:'bob',enabled:false}]};
   if(path==='/api/skills/sources/agent')return {skills:[{id:'weekly',name:'写周报',description:'整理周报'},{id:'flight',name:'订机票',description:'比较航班'}]};
   if(path==='/api/skills/sources/codex')return {skills:[]};
   if(path==='/api/skills/import'){if(exists&&!opt.body.overwrite&&opt.body.id!=='flight'){const e=new Error('库里已有同名技能，要覆盖吗？');e.status=409;throw e}return {slug:'weekly',name:'写周报'}}
   if(path.startsWith('/api/bots/')){if(saveFails){const e=new Error('x');e.status=500;throw e}return {}}
   throw new Error('unexpected '+path)};
  const host=new Node('div');
  await window.XBotSkills.mount({api,host,isCurrent:()=>true});
  assert.equal(find('sklist')[0].children.length,2);
  // search matches name and description; no match shows the plain empty message and no second import
  const search=find('sksearch')[0];
  const list=find('sklist')[0],none=find('sknone')[0];
  search.value='航班';search.oninput();
  assert.equal(list.children.length,1);assert.equal(none.hidden,true);
  search.value='不存在';search.oninput();assert.equal(list.children.length,0);assert.equal(none.hidden,false);assert.equal(list.hidden,true);
  assert.equal(all.filter(n=>n.tag==='button'&&n.textContent==='导入').length,1);
  find('clearsrch')[0].onclick();assert.equal(search.value,'');assert.equal(list.children.length,2);
  // detail: switches reflect each bot; a failed save puts the switch back
  list.children[0].onclick();await new Promise(r=>setImmediate(r));
  const togs=find('tog');assert.equal(togs.length,2);
  assert.equal(togs[0].attrs['aria-checked'],'true');assert.equal(togs[1].attrs['aria-checked'],'false');
  await togs[1].onclick();assert.equal(togs[1].attrs['aria-checked'],'true');
  assert.deepEqual(calls.at(-1).slice(0,2),['POST','/api/bots/bob/skills/weekly/enable']);
  saveFails=true;await togs[0].onclick();assert.equal(togs[0].attrs['aria-checked'],'true','failed save must restore the switch');
  assert.equal(find('sk-error').some(n=>!n.hidden&&n.textContent),true);
  // folder import: same name asks first, overwrite resends with consent, cancel does not
  find('setback')[0].onclick();
  const importBtn=all.filter(n=>n.tag==='button'&&n.textContent==='导入')[0];importBtn.onclick({currentTarget:importBtn});
  const picker=find('sk-picker')[0];
  // source layer: four rows in order, and only a cancel button in the footer
  const label=r=>r.children[1].children[0].textContent;
  const lastBox=()=>find('srcrows').at(-1);
  const lastLayer=()=>find('nestcard').at(-1);
  assert.deepEqual(lastBox().children.map(label),['Agent','Codex','Claude','本机文件夹']);
  assert.deepEqual(lastBox().children.map(r=>r.children[0].className),['bico sm ag','bico sm oa','bico sm cl','bico sm fo'],'every source row leads with its glyph');
  assert.deepEqual(find('nest-actions').at(-1).children.map(b=>b.textContent),['取消']);
  // an empty source says so and offers the way back, with no second folder button
  lastBox().children[1].onclick();await new Promise(r=>setImmediate(r));
  assert.deepEqual(calls.at(-1).slice(0,2),['GET','/api/skills/sources/codex']);
  assert.equal(find('srcempty').at(-1).children[0].textContent,'这边还没有可导入的技能。');
  assert.deepEqual(find('nest-actions').at(-1).children.map(b=>b.textContent),['取消']);
  // keyboard: the way back is reachable both directions from the cancel button
  {const back=find('srcempty').at(-1).children[1],cancel=find('nest-actions').at(-1).children[0],root=find('nestlayer').at(-1);
   const tab=(shiftKey,from)=>{document.activeElement=from;let prevented=false;root.onkeydown({key:'Tab',shiftKey,stopPropagation(){},preventDefault(){prevented=true}});return {prevented,at:document.activeElement}};
   let r=tab(false,cancel);assert.equal(r.prevented,true);assert.equal(r.at,back,'Tab from cancel wraps to the way back');
   r=tab(true,back);assert.equal(r.prevented,true);assert.equal(r.at,cancel,'Shift+Tab from the way back wraps to cancel');
   r=tab(true,cancel);assert.equal(r.prevented,false,'Shift+Tab from cancel moves to the way back by itself');}
  find('srcempty').at(-1).children[1].onclick();
  assert.deepEqual(lastBox().children.map(label),['Agent','Codex','Claude','本机文件夹']);
  // a source lists tickable skills; the one main button stays off until something is ticked
  lastBox().children[0].onclick();await new Promise(r=>setImmediate(r));
  const ck=find('cklist').at(-1), go=find('nest-actions').at(-1).children[1];
  assert.equal(go.textContent,'导入所选');assert.equal(go.disabled,true);
  assert.deepEqual(ck.children.map(r=>r.children[1].children[0].textContent),['写周报','订机票']);
  ck.children[0].onclick();assert.equal(go.disabled,false);assert.equal(ck.children[0].attrs['aria-pressed'],'true');
  ck.children[0].onclick();assert.equal(go.disabled,true);
  ck.children[0].onclick();ck.children[1].onclick();
  // same name asks; cancel skips that one and the next still goes
  const m0=calls.length;const run0=go.onclick();
  await new Promise(r=>setImmediate(r));
  assert.deepEqual({...calls[m0][2]},{source:'agent',id:'weekly',overwrite:false});
  assert.equal(all.filter(n=>n.tag==='button'&&n.textContent==='覆盖').length>=1,true);
  all.filter(n=>n.tag==='button'&&n.textContent==='取消').at(-1).onclick();
  await run0;
  assert.deepEqual({...calls[m0+1][2]},{source:'agent',id:'flight',overwrite:false});
  await new Promise(r=>setImmediate(r));
  assert.equal(find('sk-status').at(-1).textContent,'已导入「订机票」。');
  // overwrite resends the same one with consent
  const imp1=all.filter(n=>n.tag==='button'&&n.textContent==='导入')[0];imp1.onclick({currentTarget:imp1});
  lastBox().children[0].onclick();await new Promise(r=>setImmediate(r));
  find('cklist').at(-1).children[0].onclick();
  const m1=calls.length;const run1=find('nest-actions').at(-1).children[1].onclick();await new Promise(r=>setImmediate(r));
  all.filter(n=>n.tag==='button'&&n.textContent==='覆盖').at(-1).onclick();await run1;
  assert.equal(calls[m1+1][2].overwrite,true);assert.equal(calls[m1+1][2].source,'agent');
  assert.equal(find('sk-status').at(-1).textContent,'已导入「写周报」。');
  // the folder row opens the folder picker
  const imp2=all.filter(n=>n.tag==='button'&&n.textContent==='导入')[0];imp2.onclick({currentTarget:imp2});
  const before0=picker.clicked||0;lastBox().children[3].onclick();assert.equal(picker.clicked,before0+1);
  const file={webkitRelativePath:'weekly/SKILL.md',size:30,text:async()=>'---\nname: 写周报\ndescription: x\n---\n'};
  picker.files=[file];await picker.onchange();
  assert.deepEqual(calls.at(-1).slice(0,2),['POST','/api/skills/import']);assert.equal(calls.at(-1)[2].overwrite,false);assert.equal(calls.at(-1)[2].folder,'weekly');
  await new Promise(r=>setImmediate(r));
  const confirm=all.filter(n=>n.tag==='button'&&n.textContent==='覆盖').slice(-1);assert.equal(confirm.length,1);
  const n0=calls.length;
  await confirm[0].onclick();
  assert.equal(calls[n0][2].overwrite,true);
  // a folder without the skill file never reaches the server
  importBtn.onclick({currentTarget:importBtn});const before=calls.length;
  picker.files=[{webkitRelativePath:'x/readme.txt',size:1,text:async()=>''}];await picker.onchange();
  assert.equal(calls.length,before);
 finished=true;
 })().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// The phone shell shows the open skill's name and a back link to the list in its own top bar, so the
// page reports each level it enters and leaves.
func TestSkillsSettingsReportsLevels(t *testing.T) {
	source, err := staticFS.ReadFile("static/skills.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(string(source))
	script := finishGuard + `const assert=require('node:assert/strict'),vm=require('node:vm');
 const all=[];
 class Node{constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.className='';this.textContent='';this.value='';this.hidden=false;all.push(this);
  this.classList={toggle:()=>{}}}
  append(...n){this.children.push(...n)} replaceChildren(...n){this.children=n} setAttribute(k,v){this.attrs[k]=v} focus(){}}
 const window={},document={createElement:t=>new Node(t),createTextNode:t=>({textContent:t}),activeElement:null};
 vm.runInNewContext(` + string(encoded) + `,{window,document});
 (async()=>{
  const api=async path=>path==='/api/skills'?{skills:[{slug:'weekly',name:'写周报',description:'x'}]}:{slug:'weekly',name:'写周报',description:'x',bots:[]};
  const levels=[];
  await window.XBotSkills.mount({api,host:new Node('div'),isCurrent:()=>true,onLevel:l=>levels.push(l)});
  assert.deepEqual(levels,[null]);
  all.find(n=>n.className==='skrow').onclick();await new Promise(r=>setImmediate(r));
  assert.equal(levels.length,2);assert.equal(levels[1].title,'写周报');assert.equal(levels[1].backLabel,'技能');
  levels[1].back();assert.equal(levels.length,3);assert.equal(levels[2],null);
 finished=true;
 })().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// Late answers must not act: a skill read that comes back after a newer pick or the back button, and a
// folder read that finishes after the settings were closed or the import layer was replaced.
func TestSkillsSettingsIgnoresLateAnswers(t *testing.T) {
	source, err := staticFS.ReadFile("static/skills.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(string(source))
	script := finishGuard + `const assert=require('node:assert/strict'),vm=require('node:vm');
 const all=[];
 class Node{constructor(tag){this.tag=tag;this.children=[];this.attrs={};this.className='';this.textContent='';this.value='';this.hidden=false;all.push(this);
  this.classList={toggle:(n,on)=>{const s=new Set(this.className.split(' ').filter(Boolean));on?s.add(n):s.delete(n);this.className=[...s].join(' ')}}}
  append(...n){this.children.push(...n)} replaceChildren(...n){this.children=n} setAttribute(k,v){this.attrs[k]=v} focus(){} click(){}}
 const window={},document={createElement:t=>new Node(t),createTextNode:t=>({textContent:t}),activeElement:null};
 vm.runInNewContext(` + string(encoded) + `,{window,document});
 const find=(cls)=>all.filter(n=>n.className.split(' ').includes(cls));
 const tick=()=>new Promise(r=>setImmediate(r));
 const defer=()=>{let res,rej;const p=new Promise((a,b)=>{res=a;rej=b});return {p,res,rej}};
 const library=[{slug:'a',name:'甲',description:'d'},{slug:'b',name:'乙',description:'d'}];
 const detailOf=slug=>({slug,name:slug==='a'?'甲':'乙',description:'d',bots:[{name:'ada',enabled:true}]});
 const importBtn=()=>all.filter(n=>n.tag==='button'&&n.textContent==='导入').at(-1);
 (async()=>{
  // 1. newer pick wins; the earlier pick answering after "back" must not reopen its page
  {
   all.length=0;const gate={a:defer(),b:defer()};
   const api=async p=>{if(p==='/api/skills')return {skills:library};return gate[p.split('/').pop()].p};
   const host=new Node('div');await window.XBotSkills.mount({api,host,isCurrent:()=>true});
   const rows=find('skrow');rows[0].onclick();rows[1].onclick();
   gate.b.res(detailOf('b'));await tick();
   assert.equal(find('skdetail-name').at(-1).textContent,'乙');
   find('setback').at(-1).onclick();
   const nameNodes=find('skdetail-name').length;
   gate.a.res(detailOf('a'));await tick();
   assert.equal(find('skdetail-name').length,nameNodes,'late answer must not build a page');
   // a late failure stays quiet too
   const g2=defer();const api2=async p=>{if(p==='/api/skills')return {skills:library};return g2.p};
   const host2=new Node('div');await window.XBotSkills.mount({api:api2,host:host2,isCurrent:()=>true});
   find('skrow').at(-2).onclick();
   window.XBotSkills.unmount();g2.rej(Object.assign(new Error('x'),{status:500}));await tick();
   assert.equal(find('sk-error').every(n=>n.hidden||!n.textContent),true);
  }
  // 2. closing the settings while the folder is still being read: nothing is sent when the read finishes
  {
   all.length=0;const calls=[];const read=defer();
   const api=async(p,o)=>{calls.push(p);if(p==='/api/skills')return {skills:library};return {slug:'x',name:'x'}};
   const host=new Node('div');await window.XBotSkills.mount({api,host,isCurrent:()=>true});
   const btn=importBtn();btn.onclick({currentTarget:btn});
   const picker=find('sk-picker').at(-1);
   picker.files=[{webkitRelativePath:'x/SKILL.md',size:3,text:()=>read.p}];
   const pending=picker.onchange();
   window.XBotSkills.unmount();read.res('---\nname: x\n---\n');await pending;await tick();
   assert.equal(calls.includes('/api/skills/import'),false,'closed settings must not import');
  }
  // 3. cancel and reopen while the read is pending: the old read cannot use the new layer
  {
   all.length=0;const calls=[];const read=defer();
   const api=async(p,o)=>{calls.push(p);if(p==='/api/skills')return {skills:library};return {slug:'x',name:'x'}};
   const host=new Node('div');await window.XBotSkills.mount({api,host,isCurrent:()=>true});
   let btn=importBtn();btn.onclick({currentTarget:btn});
   const picker=find('sk-picker').at(-1);
   picker.files=[{webkitRelativePath:'x/SKILL.md',size:3,text:()=>read.p}];
   const pending=picker.onchange();
   all.filter(n=>n.tag==='button'&&n.textContent==='取消').at(-1).onclick();
   btn=importBtn();btn.onclick({currentTarget:btn});
   read.res('---\nname: x\n---\n');await pending;await tick();
   assert.equal(calls.includes('/api/skills/import'),false,'a read from a cancelled layer must not send');
  }
  // 4. two source picks in a row: the earlier answer arriving first must not swallow the later pick
  {
   all.length=0;const gate={agent:defer(),claude:defer()};
   const api=async p=>{if(p==='/api/skills')return {skills:library};return gate[p.split('/').pop()].p};
   const host=new Node('div');await window.XBotSkills.mount({api,host,isCurrent:()=>true});
   const btn=importBtn();btn.onclick({currentTarget:btn});
   const rows=find('srcrows').at(-1).children;rows[0].onclick();rows[2].onclick();
   gate.agent.res({skills:[{id:'a',name:'甲',description:'d'}]});await tick();
   assert.equal(find('cklist').length,0,'the older answer must not draw');
   gate.claude.res({skills:[{id:'c',name:'丙',description:'d'}]});await tick();
   assert.equal(find('nestcard').at(-1).attrs['aria-label'],'从 Claude 导入');
   // answered in the other order, the later pick still wins
   all.length=0;const g2={agent:defer(),claude:defer()};
   const api2=async p=>{if(p==='/api/skills')return {skills:library};return g2[p.split('/').pop()].p};
   await window.XBotSkills.mount({api:api2,host:new Node('div'),isCurrent:()=>true});
   const b2=importBtn();b2.onclick({currentTarget:b2});
   const r2=find('srcrows').at(-1).children;r2[0].onclick();r2[2].onclick();
   g2.claude.res({skills:[{id:'c',name:'丙',description:'d'}]});await tick();
   g2.agent.res({skills:[{id:'a',name:'甲',description:'d'}]});await tick();
   assert.equal(find('nestcard').at(-1).attrs['aria-label'],'从 Claude 导入');
   assert.equal(find('cklist').length,1);
  }
  // 5. shared bots show a plain note and no switch
  {
   all.length=0;
   const api=async p=>p==='/api/skills'?{skills:library}:{slug:'a',name:'甲',description:'d',bots:[{name:'ada',enabled:true},{name:'team',enabled:false,shared:true}]};
   const host=new Node('div');await window.XBotSkills.mount({api,host,isCurrent:()=>true});
   find('skrow')[0].onclick();await tick();
   assert.equal(find('tog').length,1);
   assert.equal(all.some(n=>n.textContent==='多人共用的 Bot，暂不使用技能库'),true);
  }
 finished=true;
 })().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// The import checklist is a flex column with a height cap. Rows must keep their own height instead of
// shrinking under their text, so a long description never runs into the next row's name. Measured in a
// real headless browser; skipped where none is installed.
func TestImportChecklistRowsDoNotOverlap(t *testing.T) {
	chrome := ""
	for _, c := range []string{"google-chrome", "chromium", "chromium-browser", "chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"} {
		if p, err := exec.LookPath(c); err == nil {
			chrome = p
			break
		}
	}
	if chrome == "" {
		t.Skip("no headless browser")
	}
	css, err := staticFS.ReadFile("static/skills.css")
	if err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><meta charset=utf-8><style>:root{--x-s5:12px;--x-fs-ui:15px;--x-fs-sm:13px}body{font:15px sans-serif;width:420px}` + string(css) + `</style>
<div class=nestcard><div class=cklist id=l></div></div><pre id=out></pre><script>
const long='使用代码化工具（以 Remotion 为核心）制作通用的动态视频、产品演示、架构演进、机制解释与数据图表动画。基于 React 逐帧数学插值，精确控制时间轴、虚拟镜头、状态重组、数据流动与导出渲染。';
for(let i=0;i<8;i++){const r=document.createElement('button');r.className='ckrow';r.innerHTML='<span class=ck></span><span class=tx><b>name'+i+'</b><span class=hint>'+long+'</span></span>';l.append(r)}
const rows=[...l.children];let bad=0;
for(const r of rows){const b=r.querySelector('b').getBoundingClientRect(),h=r.querySelector('.hint').getBoundingClientRect(),rr=r.getBoundingClientRect();
 if(h.bottom>rr.bottom+0.5||b.bottom>h.top+0.5)bad++;
 const n=r.nextElementSibling;if(n&&n.getBoundingClientRect().top<rr.bottom-0.5)bad++;
 if(h.height>2*13*1.5+1)bad++}
out.textContent='RESULT:'+(bad?'bad'+bad:'ok')+':'+l.scrollHeight+'>'+l.clientHeight;
</script>`
	f := filepath.Join(t.TempDir(), "p.html")
	if err := os.WriteFile(f, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--virtual-time-budget=2000", "--dump-dom", "file://"+f).CombinedOutput()
	if err != nil {
		t.Skipf("browser did not run: %v", err)
	}
	if !strings.Contains(string(out), "RESULT:ok:") {
		i := strings.Index(string(out), "RESULT:")
		t.Fatalf("rows overlap or overflow: %s", string(out)[max(i, 0):])
	}
	m := regexp.MustCompile(`RESULT:ok:(\d+)(?:>|&gt;)(\d+)`).FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("no measurement: %s", out)
	}
	if full, shown := atoi(m[1]), atoi(m[2]); full <= shown {
		t.Fatalf("list should scroll when rows exceed the cap: %d <= %d", full, shown)
	}
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }
