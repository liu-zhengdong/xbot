package web

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// The settings sidebar lists 人设卡 right after 技能 and before 设备.
func TestSettingsNavPutsPersonaCardsAfterSkills(t *testing.T) {
	raw, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, m := range regexp.MustCompile(`data-set="([a-z]+)"`).FindAllStringSubmatch(string(raw), -1) {
		order = append(order, m[1])
	}
	got := strings.Join(order, ",")
	if !strings.HasPrefix(got, "general,perm,search,skills,persona,devices,") {
		t.Fatalf("settings order %s", got)
	}
	if !strings.Contains(string(raw), `data-spane="persona"`) || !strings.Contains(string(raw), `src="/persona-cards.js"`) {
		t.Fatal("persona pane or script missing")
	}
}

// Behaviour of the persona-card page and picker against a fake DOM and a fake API: empty state,
// search, import as a new card with a same-name question, saving, deleting a card in use, and picking.
func TestPersonaCardsSettingsBehaviour(t *testing.T) {
	source, err := staticFS.ReadFile("static/persona-cards.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(string(source))
	script := finishGuard + `const assert=require('node:assert/strict'),vm=require('node:vm');
 let all=[];
 class Node{constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.className='';this.textContent='';this.value='';this.hidden=false;all.push(this);
  this.classList={add:n=>{this.className=(this.className+' '+n).trim()},toggle:(n,on)=>{const s=new Set(this.className.split(' ').filter(Boolean));on?s.add(n):s.delete(n);this.className=[...s].join(' ')}}}
  append(...n){this.children.push(...n)} replaceChildren(...n){this.children=n} setAttribute(k,v){this.attrs[k]=v} focus(){document.activeElement=this} click(){this.clicked=(this.clicked||0)+1}}
 const window={},document={createElement:t=>new Node(t),createTextNode:t=>({textContent:t}),activeElement:null,body:new Node('body')};
 vm.runInNewContext(` + string(encoded) + `,{window,document,setTimeout,clearTimeout});
 const find=(cls,from=all)=>from.filter(n=>n.className.split(' ').includes(cls));
 const btn=text=>all.filter(n=>n.tag==='button'&&n.textContent===text);
 const J=x=>JSON.parse(JSON.stringify(x));
 const tick=()=>new Promise(r=>setImmediate(r));
 (async()=>{
  const calls=[];let cards=[];
  const api=async(path,opt)=>{calls.push([opt?.method||'GET',path,opt?.body]);
   if(path==='/api/persona-cards'&&!opt)return {cards:structuredClone(cards)};
   if(path.startsWith('/api/persona-cards?q=')){const q=decodeURIComponent(path.slice(21)).toLowerCase();return {cards:cards.filter(c=>(c.name+' '+c.search).toLowerCase().includes(q))}}
   if(path==='/api/persona-cards/import'){if(!opt.body.confirm&&cards.some(c=>c.name==='写作助手')){const e=new Error('库里已有同名人设卡');e.status=409;throw e}
    const c={id:'c'+cards.length+'aaaaaaa',name:opt.body.confirm?'写作助手 (2)':'写作助手',preview:'先给结论',search:'先给结论'};cards.push(c);return c}
   if(path==='/api/persona-cards/c0aaaaaaa'&&!opt)return {id:'c0aaaaaaa',name:'写作助手',body:'先给结论',bots:2};
   if(path==='/api/persona-cards/c0aaaaaaa'&&opt.method==='PUT')return {id:'c0aaaaaaa',name:opt.body.name,body:opt.body.body};
   if(path.startsWith('/api/persona-cards/c0aaaaaaa')&&opt.method==='DELETE'){cards=cards.filter(c=>c.id!=='c0aaaaaaa');return {deleted:true}}
   if(path==='/api/bots/ada/persona-card')return {name:'ada',persona_card:opt.body.id?{id:opt.body.id,name:'写作助手 (2)'}:null};
   throw new Error('unexpected '+path)};
  const host=new Node('div');
  await window.XBotPersonaCards.mount({api,host,isCurrent:()=>true});
  // empty: no search or toolbar; write one (secondary) and import (primary)
  assert.equal(find('sksearch').length,0);
  assert.equal(btn('写一张')[0].className,'btn');assert.equal(btn('导入')[0].className,'btn pri');
  // import a Markdown file: a new card; the same name again asks, then imports with a suffix
  const picker=find('sk-picker')[0];
  const imp=()=>{const b=btn('导入').at(-1);b.onclick({currentTarget:b})};
  imp();assert.equal(find('nestcard').at(-1).children[0].textContent,'导入 Markdown');
  btn('选择文件').at(-1).onclick();assert.equal(picker.clicked,1);
  const file={name:'writer.md',size:20,text:async()=>'# 写作助手\n\n先给结论'};
  picker.files=[file];await picker.onchange();await tick();
  assert.deepEqual(J(calls.at(-2)),['POST','/api/persona-cards/import',{filename:'writer.md',content:'# 写作助手\n\n先给结论',confirm:false}]);
  assert.equal(find('sk-status').at(-1).textContent,'已导入「写作助手」。');
  imp();btn('选择文件').at(-1).onclick();picker.files=[file];await picker.onchange();await tick();
  const ask=find('nestcard').at(-1);assert.equal(ask.children[0].textContent,'库里已有同名人设卡');
  assert.match(ask.children[1].textContent,/「写作助手」已经有一张了/);
  await btn('仍然导入').at(-1).onclick();await tick();
  assert.equal(calls.filter(c=>c[1]==='/api/persona-cards/import').at(-1)[2].confirm,true);
  assert.equal(cards.length,2);
  // list: toolbar is search, new (icon only), import; search filters and the no-match state has no second import
  const bar=find('sktoolbar').at(-1);
  assert.deepEqual(J(bar.children.map(n=>n.className)),['sksearchwrap','btn pc-add','btn pri']);
  assert.equal(bar.children[1].textContent,'');assert.equal(bar.children[1].attrs['aria-label'],'新建人设卡');
  const search=find('sksearch').at(-1),list=find('sklist').at(-1),none=find('sknone').at(-1);
  assert.equal(list.children.length,2);
  search.value='(2)';search.oninput();assert.equal(list.children.length,1);
  search.value='没有这张';search.oninput();assert.equal(list.children.length,0);assert.equal(none.hidden,false);
  assert.equal(none.children.filter(n=>n.textContent==='导入').length,0);
  find('clearsrch').at(-1).onclick();assert.equal(list.children.length,2);
  // detail: name and one body; save sends both
  list.children[0].onclick();await tick();
  const name=find('pc-name').at(-1),text=find('pc-body').at(-1);
  assert.equal(name.value,'写作助手');assert.equal(text.value,'先给结论');
  text.value='<b>只说一句</b>';await btn('保存').at(-1).onclick();await tick();
  assert.deepEqual(J(calls.find(c=>c[0]==='PUT')),['PUT','/api/persona-cards/c0aaaaaaa',{name:'写作助手',body:'<b>只说一句</b>'}]);
  // delete a card in use: it says how many bots, then forces
  find('sklist').at(-1).children[0].onclick();await tick();
  btn('删除').at(-1).onclick({currentTarget:btn('删除').at(-1)});
  assert.match(find('nestcard').at(-1).children[1].textContent,/2 个 Bot 还在用它/);
  await btn('删除').at(-1).onclick();await tick();
  assert.deepEqual(J(calls.find(c=>c[0]==='DELETE').slice(0,2)),['DELETE','/api/persona-cards/c0aaaaaaa?force=1']);
  // picker: 未选用 first, single choice, confirm binds at once
  let picked='unset';
  await window.XBotPersonaCards.pick({api,botName:'ada',current:'',onPicked:c=>{picked=c}});
  const rows=find('pc-picklist').at(-1).children;
  assert.equal(rows[0].children[1].children[0].textContent,'未选用');assert.equal(rows[0].attrs['aria-checked'],'true');
  rows[1].onclick();
  const rows2=find('pc-picklist').at(-1).children;assert.equal(rows2[1].attrs['aria-checked'],'true');assert.equal(rows2[0].attrs['aria-checked'],'false');
  await btn('选用').at(-1).onclick();
  assert.deepEqual(J(calls.at(-1)),['PUT','/api/bots/ada/persona-card',{id:cards[0].id}]);
  assert.equal(picked.name,'写作助手 (2)');
  assert.equal(document.body.children.length,0,'picker closed');
  // nothing a card says goes into markup
  assert.equal(all.some(n=>'innerHTML' in n),false);
 finished=true;
 })().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// The picker belongs to the drawer: a list that arrives after the drawer closed,
// or after the drawer moved on, opens nothing; a second click while one is loading
// or open does nothing. Search uses the server's whole-body answer, and an older
// answer arriving late does not replace a newer one.
func TestPersonaCardPickerLateAndDeepSearch(t *testing.T) {
	source, err := staticFS.ReadFile("static/persona-cards.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(string(source))
	script := finishGuard + `const assert=require('node:assert/strict'),vm=require('node:vm');
 let all=[];
 class Node{constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.className='';this.textContent='';this.value='';this.hidden=false;all.push(this);
  this.classList={add:n=>{this.className=(this.className+' '+n).trim()},toggle:()=>{}}}
  append(...n){this.children.push(...n)} replaceChildren(...n){this.children=n} setAttribute(k,v){this.attrs[k]=v} focus(){document.activeElement=this}}
 const window={},document={createElement:t=>new Node(t),createTextNode:t=>({textContent:t}),activeElement:null,body:new Node('body')};
 vm.runInNewContext(` + string(encoded) + `,{window,document,setTimeout,clearTimeout});
 const P=window.XBotPersonaCards,find=cls=>all.filter(n=>n.className.split(' ').includes(cls));
 const wait=ms=>new Promise(r=>setTimeout(r,ms));
 const deep='x'.repeat(1024)+'关键词';
 const cards=[{id:'aaaaaaaaaaaaaaaa',name:'长卡',preview:'xxx',search:'x'.repeat(1024)},{id:'bbbbbbbbbbbbbbbb',name:'短卡',preview:'先给结论',search:'先给结论'}];
 (async()=>{
  let gets=0,release=[];
  const slow=async path=>{if(path==='/api/persona-cards'){gets++;await new Promise(r=>release.push(r));return {cards:structuredClone(cards)}}throw new Error('unexpected '+path)};
  // closed while loading: nothing opens
  let p=P.pick({api:slow,botName:'ada',current:''});
  P.closePick();release.shift()();assert.equal(await p,null);
  assert.equal(document.body.children.length,0,'late list reopened a closed picker');
  // the drawer moved on (alive false): nothing opens
  let open=true;p=P.pick({api:slow,botName:'ada',current:'',alive:()=>open});
  open=false;release.shift()();assert.equal(await p,null);
  assert.equal(document.body.children.length,0,'late list opened for a gone drawer');
  // two clicks while loading: one request, one picker
  p=P.pick({api:slow,botName:'ada',current:''});const q=P.pick({api:slow,botName:'ada',current:''});
  assert.equal(gets,3);release.shift()();const L=await p;await q;
  assert.equal(document.body.children.length,1);
  await P.pick({api:slow,botName:'ada',current:''});assert.equal(gets,3,'open picker reloaded');
  assert.equal(document.body.children.length,1);
  P.closePick();assert.equal(document.body.children.length,0);
  assert.equal(L.closed,true);
  // search past the first KiB: the server's answer wins; a stale answer is dropped
  const answers=[];
  const fast=async path=>{if(path==='/api/persona-cards')return {cards:structuredClone(cards)};
   if(path.startsWith('/api/persona-cards?q=')){const q=decodeURIComponent(path.slice(21));let r;const done=new Promise(x=>r=x);answers.push({q,r});await done;
    return {cards:cards.filter(c=>(c.name+' '+(c.id[0]==='a'?deep:c.search)).includes(q))}}
   throw new Error('unexpected '+path)};
  await P.pick({api:fast,botName:'ada',current:''});
  const search=find('pc-picksearch').at(-1),list=find('pc-picklist').at(-1);
  search.value='关键';search.oninput();await wait(150);
  search.value='关键词';search.oninput();
  assert.equal(list.children.length,1,'only 未选用 before the server answers');
  await wait(150);
  assert.deepEqual(answers.map(a=>a.q),['关键','关键词']);
  answers[1].r();await wait(0);
  assert.equal(find('pc-picklist').at(-1).children.length,2,'deep match missing');
  assert.equal(find('pc-picklist').at(-1).children[1].children[1].children[0].textContent,'长卡');
  answers[0].r();await wait(0);
  assert.equal(find('pc-picklist').at(-1).children.length,2,'stale answer replaced the newer one');
  P.closePick();
 finished=true;
 })().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
