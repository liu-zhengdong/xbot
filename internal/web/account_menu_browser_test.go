package web

import "testing"

// Exercise the shipped menu, including asynchronous device counts and the
// existing destinations. HTTP is scripted; DOM and event handlers are real.
func TestAccountMenuBrowser(t *testing.T) {
	runPageInChrome(t, `
(async()=>{
 const $=id=>document.getElementById(id), assert=(v,m)=>{if(!v)throw Error(m)};
 const until=async(f,m)=>{for(let i=0;i<200;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
 await until(()=>!$('shell').hidden,'app did not open');
 // Chrome's dump-DOM default viewport can be narrower than the desktop breakpoint.
 window.innerWidth=1440;window.dispatchEvent(new Event('resize'));
 const baseFetch=window.fetch;let pending=[];
 window.fetch=(p,o)=>p==='/api/account/devices'?new Promise(resolve=>pending.push({resolve,signal:o?.signal})):baseFetch(p,o);
 const open=async()=>{const n=pending.length;$('account').click();assert(!$('account-menu').hidden,'menu did not open');await until(()=>pending.length===n+1,'device count request missing');return pending.at(-1)};
 const count=()=>$('account-device-count');
 let req=await open();
 assert(count()&&count().hidden,'unknown device count must be hidden');
 req.resolve(json({devices:[{id:'one'},{id:'two'}]}));
 await until(()=>!count().hidden&&count().textContent==='2','count must come from account devices');
 const icons=[...$('account-menu').querySelectorAll('button')].map(b=>b.querySelector('svg'));
 assert(icons.length===6&&icons.every(Boolean),'every action needs its icon');
 $('collapse').click();await open();
 assert($('sidebar').classList.contains('rail'),'collapse did not change sidebar');
 assert($('collapse').querySelector('svg')&&$('collapse').textContent==='展开列表','expand lost icon or action');
 $('collapse').click();req=await open();
 req.resolve(json({devices:[]}));await until(()=>!count().hidden&&count().textContent==='0','empty successful list is a real zero');
 $('account').click();req=await open();req.resolve(new Response('{}',{status:401}));
 await new Promise(r=>setTimeout(r,30));assert(count().hidden&&!$('shell').hidden,'account failure must hide count without logging out local owner');
 $('account').click();const old=await open();$('account').click();req=await open();
 assert(old.signal.aborted,'closing menu must cancel request');
 req.resolve(json({devices:[{id:'new'}]}));await until(()=>count().textContent==='1','latest count missing');
 old.resolve(json({devices:[{id:'stale'},{id:'stale2'}]}));await new Promise(r=>setTimeout(r,30));
 assert(count().textContent==='1','late count replaced latest result');
 $('account').click();req=await open();req.resolve(json({}));await new Promise(r=>setTimeout(r,30));assert(count().hidden,'malformed count must be hidden');
 const calls=[];window.XBotAccount.show=(...v)=>calls.push(v);window.XBotAccount.signOut=async()=>calls.push(['logout']);
 $('account-devices').click();assert(calls.at(-1)[0]==='devices'&&$('account-menu').hidden,'devices destination changed');
 await open();$('account-gateway').click();assert(calls.at(-1)[0]==='gateway'&&calls.at(-1)[1].from==='app','gateway destination changed');
 await open();$('account-settings').click();assert(!$('setpage').hidden,'settings destination changed');
 document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
 await open();$('account-about').click();assert(!$('setpage').hidden&&getComputedStyle(document.querySelector('[data-spane=about]')).display!=='none','about destination changed');
 document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
 await open();$('logout').click();assert(calls.at(-1)[0]==='logout'&&$('account-menu').hidden,'logout destination changed');
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
