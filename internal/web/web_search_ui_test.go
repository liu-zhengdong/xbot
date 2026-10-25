package web

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestSearchSelectionRefreshesCredentialStatusWithoutLosingDraft(t *testing.T) {
	source, err := staticFS.ReadFile("static/web-search.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(string(source))
	script := `const assert=require('node:assert/strict'),vm=require('node:vm');
 const nodes=[];
 class Node {
  constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.className='';this.value='';this.hidden=false;nodes.push(this);this.classList={toggle:(name,on)=>{const s=new Set(this.className.split(' ').filter(Boolean));on?s.add(name):s.delete(name);this.className=[...s].join(' ')}}}
  append(...nodes){this.children.push(...nodes)}
  replaceChildren(...nodes){this.children=nodes}
  setAttribute(k,v){this.attrs[k]=v}
 }
 const window={},document={createElement:tag=>new Node(tag)};
 vm.runInNewContext(` + string(encoded) + `,{window,document,AbortController});
 const providers=[{id:'ddg',name:'DuckDuckGo',needs_key:false,key_configured:false},{id:'deepseek',name:'DeepSeek',needs_key:true,key_configured:false}];
 const host=new Node('div');let state={enabled:true,mode:'auto',provider:'ddg',providers};
 const api=async(path,opt)=>{if(opt?.method==='POST')throw new Error('fake rejected');if(opt?.method==='PUT'){state={...state,...opt.body,providers:providers.map(p=>({...p,key_configured:p.id==='deepseek'}))}};return structuredClone(state)};
 (async()=>{
  await window.XBotWebSearch.mount({api,host,isCurrent:()=>true});
  const provider=id=>nodes.find(n=>n.dataset.provider===id);
  const input=nodes.find(n=>n.id==='search-key-deepseek');
  const tryButton=nodes.find(n=>n.className==='ws-try');
  assert.equal(tryButton.disabled,true);
  await provider('deepseek').onclick();
  assert.equal(Boolean(tryButton.disabled),false,'new server credential state was ignored');
  assert.equal(input.value,'','server echoed credential');
  await tryButton.onclick();
  const status=nodes.find(n=>n.attrs.role==='status'),failure=status.textContent;
  await nodes.find(n=>n.dataset.mode==='local').onclick();
  assert.equal(status.textContent,failure,'mode change hid known credential failure');
  input.value='unsubmitted-draft';input.oninput();
  await provider('ddg').onclick();await provider('deepseek').onclick();
  assert.equal(input.value,'unsubmitted-draft','selection overwrote user draft');
  assert.equal(Boolean(tryButton.disabled),false);
  // A settings response may predate a concurrently verified credential.
  nodes.length=0;let finishProbe,finishSave;
  const initial={enabled:true,mode:'auto',provider:'deepseek',providers:structuredClone(providers)};
  const concurrent=async(path,opt)=>{
   if(opt?.method==='POST')return new Promise(resolve=>finishProbe=resolve);
   if(opt?.method==='PUT')return new Promise(resolve=>finishSave=()=>resolve({...structuredClone(initial),...opt.body}));
   return structuredClone(initial);
  };
  await window.XBotWebSearch.mount({api:concurrent,host:new Node('div'),isCurrent:()=>true});
  const input2=nodes.find(n=>n.id==='search-key-deepseek'),try2=nodes.find(n=>n.className==='ws-try'),status2=nodes.find(n=>n.attrs.role==='status');
  input2.value='fake-verified-key';input2.oninput();
  const probe=try2.onclick();const save=nodes.find(n=>n.dataset.mode==='local').onclick();
  finishProbe({ok:true});await probe;const success=status2.textContent;
  finishSave();await save;
  assert.equal(status2.textContent,success,'older settings response hid a verified key');
  assert.equal(Boolean(try2.disabled),false,'verified stored key could not be retried');
  assert.equal(input2.value,'');
 })().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
