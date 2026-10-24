package web

import (
	"os/exec"
	"testing"
)

func TestChatGPTSubscriptionLoginStates(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	const script = `
const fs=require('fs'),vm=require('vm'),assert=require('node:assert/strict');
let completed=false;process.on('beforeExit',()=>{if(!completed){console.error('async test did not complete');process.exitCode=1}});
class Element {
 constructor(tag){this.tag=tag;this.children=[];this.attrs={};this.classList={add(){}};this.textContent='';}
 append(...nodes){this.children.push(...nodes)} replaceChildren(...nodes){this.children=nodes}
 setAttribute(k,v){this.attrs[k]=v} focus(){} get innerText(){return this.textContent+this.children.map(n=>n.innerText).join(' ')}
}
const host=new Element('main'),calls=[],timers=[],popups=[];
let native=false,nativeOpens=[],blocked=false,failStart=false,failStatus=false,failRefresh=false,configured=false,current=true,resolveStatus;
const window={open(url){if(native){assert(url.startsWith('https://'));nativeOpens.push(url);return null}assert(url==='about:blank'||url==='https://auth.example/authorize');if(blocked)return null;const popup={closed:false,opener:{},location:{replace(url){popup.url=url}},close(){this.closed=true}};popups.push(popup);return popup}};
const document={documentElement:{classList:{contains:()=>native}},createElement:tag=>new Element(tag),createElementNS:(_,tag)=>new Element(tag)};
vm.runInNewContext(fs.readFileSync('static/providers.js','utf8'),{window,document,URL,WeakMap,encodeURIComponent,setTimeout:fn=>timers.push(fn)});
const walk=n=>[n,...n.children.flatMap(walk)],find=(tag,text)=>walk(host).find(n=>n.tag===tag&&n.innerText.includes(text));
const api=async(path,opts)=>{
 calls.push({path,opts});
 if(path==='/api/providers'){if(failRefresh)throw Error('private-upstream-body');return {providers:configured?[{name:'account',oauth:'chatgpt',key_configured:true,protocol:'responses',models:[]}]:[]}}
 if(path.endsWith('/start')){assert.equal(opts.method,'POST');if(failStart)throw Error('private-upstream-body');return {url:'https://auth.example/authorize'}}
 if(path.endsWith('/status')){if(failStatus)throw Error('private-upstream-body');return await new Promise(resolve=>resolveStatus=resolve)}
 assert.equal(path,'/api/providers/models?provider=account');return {models:[{model:'runtime-only',context:272000,vision:true,hosted_web_search:true}]};
};
const flush=async()=>{for(let i=0;i<8;i++)await Promise.resolve()};
const startCount=()=>calls.filter(c=>c.path.endsWith('/start')).length;
(async()=>{
 await window.XBotProviders.settings({api,host,isCurrent:()=>current});
 assert(!find('button','ChatGPT').disabled);assert.equal(startCount(),0);
 const input=find('input','');input.value='chatgpt';input.oninput();assert(!walk(host).find(n=>n.tag==='section'&&n.innerText.includes('订阅')).hidden);
 const card=find('button','ChatGPT');const attempt=card.onclick();await flush();
 assert.equal(startCount(),1);assert(!card.disabled);await card.onclick();assert.equal(startCount(),1);
 assert.equal(popups[0].opener,null);assert.equal(popups[0].url,'https://auth.example/authorize');assert.equal(popups.at(-1).opener,null);
 resolveStatus({state:'pending'});await flush();assert.equal(timers.length,1);assert.equal(find('input',''),input,'pending repainted search');
 timers.shift()();await flush();configured=true;resolveStatus({state:'succeeded'});await attempt;
 assert(walk(card).some(n=>n.attrs['aria-label']==='已登录'));assert(!card.disabled);assert.equal(find('input',''),input);
 assert.equal(walk(host).filter(n=>n.tag==='button'&&n.innerText.includes('ChatGPT')).length,1,'duplicate subscription card');
 await card.onclick();assert(host.innerText.includes('已登录'));assert(host.innerText.includes('runtime-only'));assert(!walk(host).some(n=>n.tag==='table'));assert(find('button','重新登录'));
 const again=find('button','重新登录'),late=again.onclick();await flush();find('button','模型与供应商').onclick();
 resolveStatus({state:'succeeded'});await late;assert(find('input',''),'late status replaced list');assert(!host.innerText.includes('runtime-only'));
 await find('button','ChatGPT').onclick();const failed=find('button','重新登录').onclick();await flush();resolveStatus({state:'failed',error:'private-upstream-body'});await failed;
 assert(host.innerText.includes('登录未完成'));assert(!host.innerText.includes('private-upstream-body'));assert(!find('button','重新登录').disabled);
 const closeAttempt=find('button','重新登录').onclick();await flush();popups.at(-1).closed=true;resolveStatus({state:'pending'});await flush();assert(!find('button','重新登录').disabled);assert(host.innerText.includes('等待登录'),'closed WindowProxy stopped server polling');
 const reopenBefore=startCount();await find('button','重新登录').onclick();assert.equal(startCount(),reopenBefore);timers.shift()();await flush();resolveStatus({state:'succeeded'});await closeAttempt;await flush();assert(host.innerText.includes('runtime-only'));
 failStart=true;await find('button','重新登录').onclick();assert(!host.innerText.includes('private-upstream-body'));failStart=false;
 failStatus=true;await find('button','重新登录').onclick();assert(!find('button','重新登录').disabled);failStatus=false;
 const refresh=find('button','重新登录').onclick();await flush();failRefresh=true;resolveStatus({state:'succeeded'});await refresh;failRefresh=false;assert(host.innerText.includes('登录未完成'));
 blocked=true;const before=startCount();await find('button','重新登录').onclick();assert.equal(startCount(),before);blocked=false;
 const closed=find('button','重新登录').onclick();await flush();current=false;resolveStatus({state:'succeeded'});await closed;assert(!host.innerText.includes('private-upstream-body'));
 current=true;await window.XBotProviders.settings({api,host});const stale=find('button','ChatGPT').onclick();await stale;
 const remount=find('button','重新登录').onclick();await flush();await window.XBotProviders.settings({api,host});resolveStatus({state:'failed'});await remount;assert(!host.innerText.includes('登录未完成'),'old mount changed new mount');
 native=true;configured=false;await window.XBotProviders.settings({api,host});const nativeBefore=startCount(),nativeLogin=find('button','ChatGPT').onclick();await flush();assert.equal(nativeOpens.length,1);assert(!find('button','ChatGPT').disabled);await find('button','ChatGPT').onclick();assert.equal(nativeOpens.length,2);assert.equal(startCount(),nativeBefore+1);configured=true;resolveStatus({state:'succeeded'});await nativeLogin;assert(walk(host).some(n=>n.attrs['aria-label']==='已登录'));completed=true;
})().catch(e=>{console.error(e);process.exitCode=1});`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("ChatGPT UI: %v: %s", err, out)
	}
}
