package web

import "testing"

// 侧栏把同一账号里其他电脑的 Bot 并进远端那一组：登录后按设备分组列出来，
// 本机那台不算；账号设备的对话通道还没打通，点开只在面板里说明，不发请求。
func TestAccountRosterSidebarBrowser(t *testing.T) {
	runTodoBrowser(t, accountRosterSidebarFixture, accountRosterSidebarDriver)
}

// 没有账号（或没登录）时侧栏维持原样：不读账号设备，那一组也不出现。
func TestAccountRosterSignedOutBrowser(t *testing.T) {
	runTodoBrowser(t, accountRosterSignedOutFixture, accountRosterSignedOutDriver)
}

const accountRosterDevices = `
const rosterDevices=[
 {id:'mini',name:'Mac mini',online:true,self:true,bots:[],os:'darwin',capabilities:[],last_seen:'2026-11-04T12:00:00Z',running_count:0,chat:false},
 {id:'desk',name:'工作电脑',online:true,self:false,bots:[{name:'writer'},{name:'editor'}],os:'darwin',capabilities:[],last_seen:'2026-11-04T11:58:00Z',running_count:0,chat:false},
 {id:'night',name:'夜里的笔记本',online:false,self:false,bots:[{name:'keeper'}],os:'darwin',capabilities:[],last_seen:'2026-11-03T09:00:00Z',running_count:0,chat:false}
];
`

const accountRosterSidebarFixture = accountRosterDevices + `
const rosterCalls=[];
const rosterBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const url=String(path);
 if(url==='/api/devices')return json({self:{name:'本机',os:'macOS',bots:bots.map(b=>b.name)},devices:[],pairing:{a:{},b:{}},gateway_ok:true});
 if(url==='/api/account/devices')return json({devices:rosterDevices.map(d=>({...d}))});
 if(/^\/api\/(remote|account\/devices\/[^/]+\/bots)\//.test(url)){rosterCalls.push(url);return json({messages:[],device:{id:'desk'}})}
 return rosterBaseFetch(path,opts);
};
`

// 断的是行为：另一台电脑的 Bot 出现在侧栏、点开只说明不请求、发也发不出去；
// 本机的 Bot 不会被当成人家的。设备名和组头文案不在这里钉。
const accountRosterSidebarDriver = `
(async()=>{
 const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
 const until=async(fn,msg)=>{for(let i=0;i<700;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
 const botButtons=()=>[...document.querySelectorAll('#remote-roster [data-side="open"]')];
 const botFor=name=>botButtons().find(n=>(n.dataset.bot||'')===name);
 await until(()=>!$('#shell').hidden&&!$('.thread-loading-cover'),'boot');
 await until(()=>botFor('writer'),'the other computer\'s bots never reached the sidebar');
 const names=botButtons().map(n=>n.dataset.bot).sort();
 assert(JSON.stringify(names)===JSON.stringify(['editor','keeper','writer']),'sidebar bots: '+names.join(','));
 assert(!botFor('XBot'),'this computer\'s own bot was listed as another one');
 // 点开：面板起来，说明一句就够，一个对话请求都不发。
 botFor('writer').click();
 await until(()=>!$('#remote-chat').hidden,'remote panel did not open');
 await until(()=>$('#rc-thread')&&$('#rc-thread').textContent.trim().length>0,'panel said nothing');
 assert(rosterCalls.length===0,'a chat request went out without a channel: '+rosterCalls.join(','));
 // 打字发送同样不出门，原文留在手里。
 $('#rc-input').value='在吗';$('#rc-form').requestSubmit();
 await until(()=>$('#rc-error').textContent.trim().length>0,'send said nothing');
 assert(rosterCalls.length===0,'send went out without a channel: '+rosterCalls.join(','));
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`

const accountRosterSignedOutFixture = accountRosterDevices + `
const signedOutCalls=[];
const signedOutBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const url=String(path);
 if(url==='/api/account/me')return json({account:null});
 if(url==='/api/devices')return json({self:{name:'本机',os:'macOS',bots:bots.map(b=>b.name)},devices:[],pairing:{a:{},b:{}},gateway_ok:true});
 if(url==='/api/account/devices'){signedOutCalls.push(url);return new Response(JSON.stringify({error:'unauthorized'}),{status:401,headers:{'Content-Type':'application/json'}})}
 return signedOutBaseFetch(path,opts);
};
`

const accountRosterSignedOutDriver = `
(async()=>{
 const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
 const until=async(fn,msg)=>{for(let i=0;i<700;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
 await until(()=>!$('#shell').hidden&&!$('.thread-loading-cover'),'boot');
 await new Promise(r=>setTimeout(r,120));
 assert(!document.querySelectorAll('#remote-roster [data-side="open"]').length,'signed-out sidebar offers another computer\'s bots');
 assert(signedOutCalls.length===0,'signed-out sidebar read the account roster');
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`

// 通道打开（账号设备行带 chat）时点开：请求走账号设备那条路由，不回落到配对设备的
// /api/remote；读回来的对话按角色显示在面板里。
func TestAccountRosterChatRouteBrowser(t *testing.T) {
	runTodoBrowser(t, accountRosterChannelFixture, accountRosterChannelDriver)
}

const accountRosterChannelFixture = accountRosterDevices + `
const channelCalls=[];
const channelBaseFetch=window.fetch;
const channelDevices=rosterDevices.map(d=>({...d,chat:!d.self}));
window.fetch=async(path,opts={})=>{
 const url=String(path);
 if(url==='/api/devices')return json({self:{name:'本机',os:'macOS',bots:bots.map(b=>b.name)},devices:[],pairing:{a:{},b:{}},gateway_ok:true});
 if(url==='/api/account/devices')return json({devices:channelDevices.map(d=>({...d}))});
 if(/^\/api\/(remote|account\/devices\/[^/]+\/bots)\//.test(url)){
  channelCalls.push(url);
  if(/^\/api\/account\/devices\/desk\/bots\/writer\/messages/.test(url))
   return json({messages:[{id:'7',role:'me',text:'在吗',at:'2026-11-04T12:00:00Z',state:'delivered'}],device:{id:'desk',label:'工作电脑',online:true}});
  return json({messages:[],device:{id:'desk'}});
 }
 return channelBaseFetch(path,opts);
};
`

const accountRosterChannelDriver = `
(async()=>{
 const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
 const until=async(fn,msg)=>{for(let i=0;i<700;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
 const botFor=name=>[...document.querySelectorAll('#remote-roster [data-side="open"]')].find(n=>(n.dataset.bot||'')===name);
 await until(()=>!$('#shell').hidden&&!$('.thread-loading-cover'),'boot');
 await until(()=>botFor('writer'),'the other computer\'s bots never reached the sidebar');
 botFor('writer').click();
 await until(()=>channelCalls.some(u=>u.startsWith('/api/account/devices/desk/bots/writer/messages')),'no account-channel read: '+channelCalls.join(','));
 assert(!channelCalls.some(u=>u.startsWith('/api/remote/')),'fell back to the paired route: '+channelCalls.join(','));
 await until(()=>$('#rc-thread').textContent.includes('在吗'),'history did not render');
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`
