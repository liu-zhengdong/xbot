package web

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// Behaviour of the settings page script against a fake DOM and a fake API: filtering, the same-name
// question before overwriting, and an on/off switch that falls back when saving fails.
func TestSkillsSettingsBehaviour(t *testing.T) {
	source, err := staticFS.ReadFile("static/skills.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(string(source))
	script := `const assert=require('node:assert/strict'),vm=require('node:vm');
 const all=[];
 class Node{constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.className='';this.textContent='';this.value='';this.hidden=false;all.push(this);
  this.classList={toggle:(n,on)=>{const s=new Set(this.className.split(' ').filter(Boolean));on?s.add(n):s.delete(n);this.className=[...s].join(' ')}}}
  append(...n){this.children.push(...n)} replaceChildren(...n){this.children=n} setAttribute(k,v){this.attrs[k]=v} focus(){} click(){this.clicked=(this.clicked||0)+1}}
 const window={},document={createElement:t=>new Node(t),createTextNode:t=>({textContent:t}),activeElement:null};
 vm.runInNewContext(` + string(encoded) + `,{window,document});
 const find=(cls,from=all)=>from.filter(n=>n.className.split(' ').includes(cls));
 const library=[{slug:'weekly',name:'写周报',description:'整理周报'},{slug:'flight',name:'订机票',description:'比较航班'}];
 (async()=>{
  const calls=[];let saveFails=false,exists=true;
  const api=async(path,opt)=>{calls.push([opt?.method||'GET',path,opt?.body]);
   if(path==='/api/skills')return {skills:structuredClone(library)};
   if(path==='/api/skills/weekly')return {slug:'weekly',name:'写周报',description:'整理周报',bots:[{name:'ada',enabled:true},{name:'bob',enabled:false}]};
   if(path==='/api/skills/import'){if(exists&&!opt.body.overwrite){const e=new Error('库里已有同名技能，要覆盖吗？');e.status=409;throw e}return {slug:'weekly',name:'写周报'}}
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
  // import: same name asks first, overwrite resends with consent, cancel does not
  find('setback')[0].onclick();
  const importBtn=all.filter(n=>n.tag==='button'&&n.textContent==='导入')[0];importBtn.onclick({currentTarget:importBtn});
  const picker=find('sk-picker')[0];
  const file={webkitRelativePath:'weekly/SKILL.md',size:30,text:async()=>'---\nname: 写周报\ndescription: x\n---\n'};
  picker.files=[file];await picker.onchange();
  assert.deepEqual(calls.at(-1).slice(0,2),['POST','/api/skills/import']);assert.equal(calls.at(-1)[2].overwrite,false);assert.equal(calls.at(-1)[2].folder,'weekly');
  await new Promise(r=>setImmediate(r));
  const confirm=all.filter(n=>n.tag==='button'&&n.textContent==='覆盖');assert.equal(confirm.length,1);
  const n0=calls.length;
  await confirm[0].onclick();
  assert.equal(calls[n0][2].overwrite,true);
  // a folder without the skill file never reaches the server
  importBtn.onclick({currentTarget:importBtn});const before=calls.length;
  picker.files=[{webkitRelativePath:'x/readme.txt',size:1,text:async()=>''}];await picker.onchange();
  assert.equal(calls.length,before);
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
	script := `const assert=require('node:assert/strict'),vm=require('node:vm');
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
	script := `const assert=require('node:assert/strict'),vm=require('node:vm');
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
  // 4. shared bots show a plain note and no switch
  {
   all.length=0;
   const api=async p=>p==='/api/skills'?{skills:library}:{slug:'a',name:'甲',description:'d',bots:[{name:'ada',enabled:true},{name:'team',enabled:false,shared:true}]};
   const host=new Node('div');await window.XBotSkills.mount({api,host,isCurrent:()=>true});
   find('skrow')[0].onclick();await tick();
   assert.equal(find('tog').length,1);
   assert.equal(all.some(n=>n.textContent==='多人共用的 Bot，暂不使用技能库'),true);
  }
 })().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
