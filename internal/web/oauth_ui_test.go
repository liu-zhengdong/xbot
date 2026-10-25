package web

import (
	"os/exec"
	"testing"
)

func TestProvidersSettingsCatalogStatesAndLateReplies(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	const script = `
const fs=require('fs'),vm=require('vm'),assert=require('node:assert/strict');
class Element {
 constructor(tag){this.tag=tag;this.children=[];this.attrs={};this.classList={add(){}};this.textContent='';}
 append(...nodes){this.children.push(...nodes)} replaceChildren(...nodes){this.children=nodes}
 setAttribute(k,v){this.attrs[k]=v} focus(){} get innerText(){return this.textContent+this.children.map(n=>n.innerText).join(' ')}
}
const host=new Element('main'),calls=[];
const window={open(){throw Error('OAuth must stay disabled')}};
const document={createElement:tag=>new Element(tag),createElementNS:(_,tag)=>new Element(tag)};
vm.runInNewContext(fs.readFileSync('static/providers.js','utf8'),{window,document,URL,WeakMap,encodeURIComponent});
const walk=n=>[n,...n.children.flatMap(walk)];
const find=(tag,text)=>walk(host).find(n=>n.tag===tag&&n.innerText.includes(text));
const provider={name:'openai',protocol:'responses',key_configured:true,models:[{model:'local-only'}]};
let resolveList,fail=false,empty=false,current=true;
const api=async path=>{calls.push(path);if(path==='/api/providers')return {providers:[provider,...['constructor','__proto__'].map(name=>({name,protocol:'chat_completions',key_configured:false,models:[]}))]};if(fail)throw Error('private-upstream-body');return await new Promise(resolve=>{resolveList=resolve})};
const result={models:[{model:'runtime-only',context:128000,vision:false,hosted_web_search:false}]};
(async()=>{
 await window.XBotProviders.settings({api,host,isCurrent:()=>current});
 assert(find('button','constructor'));assert(find('button','__proto__'));assert(!find('button','ChatGPT').disabled);assert(!calls.some(p=>p.includes('/oauth/')));
 const input=find('input','');input.value='OpenAI';input.oninput();assert(!find('button','DeepSeek'));
 let request=find('button','OpenAI').onclick();assert(host.innerText.includes('正在拉取'));
 find('button','模型与供应商').onclick();assert.equal(find('input','').value,'OpenAI');
 resolveList(result);await request;assert(!host.innerText.includes('runtime-only'),'late detail replaced list');
 request=find('button','OpenAI').onclick();resolveList(result);await request;
 assert(host.innerText.includes('runtime-only'));assert(!host.innerText.includes('local-only'));assert(host.innerText.includes('无厂商搜索'));assert(host.innerText.includes('不能看图'));assert(!/hosted|vision|API_KEY|web\.json|providers\.json/.test(host.innerText),'engineering words in detail');
 assert(!walk(host).some(n=>n.tag==='table'));
 find('button','模型与供应商').onclick();fail=true;await find('button','OpenAI').onclick();assert(find('button','重试'));assert(!host.innerText.includes('private-upstream-body'));
 fail=false;request=find('button','重试').onclick();await Promise.resolve();await Promise.resolve();resolveList({models:[]});await request;assert(host.innerText.includes('未返回可用模型'));assert(!/hosted|vision/.test(host.innerText));assert(find('button','重试'));
 find('button','模型与供应商').onclick();provider.key_configured=false;await find('button','OpenAI').onclick();assert(host.innerText.includes('请先'));assert(find('button','重试'));
 provider.key_configured=true;request=find('button','重试').onclick();await Promise.resolve();await Promise.resolve();current=false;resolveList(result);await request;assert(!host.innerText.includes('runtime-only'),'closed settings received late reply');
})().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("providers behavior: %v: %s", err, out)
	}
}
