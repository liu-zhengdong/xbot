package web

import "testing"

func TestAccountSharedDevicesBrowser(t *testing.T) {
	runTodoBrowser(t, accountSharedDevicesFixture, accountSharedDevicesDriver)
}

func TestAccountSharedDeviceTargetBrowser(t *testing.T) {
	runTodoBrowser(t, accountSharedDevicesFixture, accountSharedDeviceTargetDriver)
}

func TestAccountSharedDeviceAvailabilityBrowser(t *testing.T) {
	runTodoBrowser(t, accountSharedDevicesFixture, accountSharedDeviceAvailabilityDriver)
}

func TestAccountSharedDevicesLocalBrowser(t *testing.T) {
	runTodoBrowser(t, accountSharedDevicesFixture+accountSharedDevicesLocalFixture, accountSharedDevicesLocalDriver)
}

const accountSharedDevicesFixture = `
localStorage.removeItem('xbot.side');bots.forEach(b=>b.pinned=false);
if(new URLSearchParams(location.search).get('theme')==='dark')localStorage.setItem('xbot-theme','dark');
const accountFixture={reads:0,detailReads:0,taskReads:0,unavailable:[],tasksTruncated:false,detailTruncated:false,failList:false,failRename:false,failDelete:false,failSend:false,failStop:false,holdDetail:false,releaseDetail:null,writes:[],sends:[],artifactReads:[],devices:[
 {id:'mini',name:'Mac mini',online:true,self:true,bots:[],os:'darwin',capabilities:['read','write','exec','exec:zsh','exec:bash','exec:sh','browser','desktop'],last_seen:'2026-11-04T12:00:00Z',running_count:2},
 {id:'windows',name:'Windows 工作站',online:true,self:false,bots:[],os:'windows',capabilities:['read','write','exec','exec:powershell','exec:cmd'],running_count:0},
 {id:'offline',name:'离线长名称 MacBook Pro 工作电脑',online:false,self:false,bots:[],os:'darwin',capabilities:['read'],running_count:0}
],tasks:[
 {id:'t1',ref:'job/ref-1',device_id:'mini',device_name:'Mac mini',bot:bots[0].name,context_id:'ctx-1',kind:'shell',description:'检查界面组件',status:'running',started_at:'2026-11-04T12:00:00Z',artifacts:[]},
 {id:'t2',ref:'job/ref-2',device_id:'mini',device_name:'Mac mini',bot:bots[1].name,context_id:'ctx-2',kind:'shell',description:'整理文档目录',status:'running',started_at:'2026-11-04T12:00:00Z',artifacts:[]}
]};
const deviceFail=message=>new Response(JSON.stringify({error:message}),{status:503,headers:{'Content-Type':'application/json'}});
const accountBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const url=String(path),method=opts.method||'GET';
 if(url==='/api/account/devices'){
  accountFixture.reads++;if(accountFixture.failList)return deviceFail('设备读取失败');
  return json({devices:accountFixture.devices.map(d=>({...d}))});
 }
 if(url==='/api/devices')return json({self:{name:'本机',os:'macOS',bots:bots.map(b=>b.name)},devices:[],pairing:{a:{},b:{}},gateway_ok:true});
 if(url==='/api/devices/desktop')return json({platform:'darwin',status:{data:{permissions:{accessibility:{state:'granted'}}}}});
 const device=url.match(/^\/api\/account\/devices\/([^/]+)$/);
 if(device){
  const id=decodeURIComponent(device[1]),d=accountFixture.devices.find(d=>d.id===id);
  if(method==='PATCH'){
   const body=JSON.parse(opts.body);accountFixture.writes.push({method,id,body});
   if(accountFixture.failRename)return deviceFail('设备改名失败');d.name=body.name;return json({device:{...d}});
  }
  if(method==='DELETE'){
   accountFixture.writes.push({method,id});if(accountFixture.failDelete)return deviceFail('解绑失败');
   accountFixture.devices=accountFixture.devices.filter(d=>d.id!==id);return json({unbound:true});
  }
  accountFixture.detailReads++;
  if(accountFixture.holdDetail)await new Promise(resolve=>accountFixture.releaseDetail=resolve);
  return json({device:{...d},tasks:accountFixture.tasks.filter(t=>t.device_id===id).map(t=>({...t})),tasks_truncated:accountFixture.detailTruncated});
 }
 const tasks=url.match(/^\/api\/bots\/([^/]+)\/device-tasks$/);
 if(tasks){
  accountFixture.taskReads++;
  return json({tasks:accountFixture.tasks.filter(t=>t.bot===decodeURIComponent(tasks[1])&&!accountFixture.unavailable.some(d=>d.id===t.device_id)).map(t=>({...t})),unavailable_devices:accountFixture.unavailable.map(d=>({...d})),tasks_truncated:accountFixture.tasksTruncated});
 }
 const task=url.match(/^\/api\/account\/devices\/([^/]+)\/tasks\/([^/?]+)(.*)$/);
 if(task){
  const t=accountFixture.tasks.find(t=>t.device_id===decodeURIComponent(task[1])&&t.ref===decodeURIComponent(task[2]));
  if(accountFixture.unavailable.some(d=>d.id===t.device_id))return deviceFail('设备已离线');
  if(task[3]==='/stop'){
   accountFixture.writes.push({method,ref:t.ref});if(accountFixture.failStop)return deviceFail('停止失败');t.status='cancelled';return json({task:{...t}});
  }
  if(task[3].startsWith('/artifacts/')){
   const q=new URL(url,'http://fixture').searchParams,offset=Number(q.get('offset')),limit=Number(q.get('limit'));
   accountFixture.artifactReads.push({offset,limit});return new Response(new Uint8Array(Math.min(limit,40000-offset)).fill(65));
  }
  const offset=Number(new URL(url,'http://fixture').searchParams.get('offset')||0),output='实际执行输出\n'.slice(offset);
  return json({task:{...t},output,next_offset:offset+output.length});
 }
 if(/^\/api\/bots\/[^/]+\/messages$/.test(url)){
  accountFixture.sends.push(JSON.parse(opts.body));
  if(accountFixture.failSend)return deviceFail('目标设备已离线');
 }
 return accountBaseFetch(path,opts);
};
`

const accountSharedDevicesDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const until=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const click=s=>{const n=$(s);assert(n&&n.getClientRects().length,'missing '+s);n.focus();n.click()};
const keyEscape=()=>document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));
(async()=>{
 await until(()=>!$('#shell').hidden&&!$('.thread-loading-cover'),'boot');
 const shot=new URLSearchParams(location.search).get('shot');
 if(shot){
  await window.XBotSettings.open('devices');await until(()=>$('[data-shared=detail]'),'shot devices');
  if(shot==='detail'){click('[data-shared=detail]');await until(()=>$('[data-shared=unbind]'),'shot detail');document.activeElement.blur()}
  if(shot==='picker'||shot==='task'){
   window.XBotSettings.close();await until(()=>!$('#account-device-foot').hidden,'shot bot');
   click(shot==='picker'?'#account-device-trigger':'#account-device-task-status');
   await until(()=>$('#account-devices-modal dialog'),'shot dialog');
  }
  document.body.dataset.testResult='passed';return;
 }
 if(new URLSearchParams(location.search).has('account-mobile')){
  click('#menu');click('#account');click('#account-settings');
  await until(()=>!$('#setpage').hidden,'mobile settings');
  assert($('.setnav').getClientRects().length&&!$('.setmain').getClientRects().length,'directory absent');
  click('[data-set=devices]');await until(()=>$('[data-shared=detail]'),'mobile devices');
  const row=$('[data-shared=detail]'),r=row.getBoundingClientRect();
  assert(r.height>=64&&r.right<=390,'mobile row clipped');
  assert(row.querySelector('.account-device-presence').getBoundingClientRect().width>0&&row.lastElementChild.getBoundingClientRect().width>0,'task count or arrow hidden');
  click('[data-shared=detail]');await until(()=>$('[data-shared=unbind]'),'mobile detail');
  const cnt=$('.setcnt');cnt.scrollTop=cnt.scrollHeight;
  const unbind=$('[data-shared=unbind]');unbind.scrollIntoView({block:'nearest'});
  assert(unbind.getBoundingClientRect().bottom<=cnt.getBoundingClientRect().bottom+1,'unbind unreachable');
  click('[data-shared=unbind]');keyEscape();
  assert(!$('#setpage').hidden&&document.activeElement===unbind,'Escape failed nested focus');
  click('#settings-back');assert($('.setnav').getClientRects().length,'directory return');
  click('#settings-close-m');assert($('#setpage').hidden,'mobile close');
  document.body.dataset.testResult='passed';return;
 }
 $('#account').focus();await window.XBotSettings.open('devices');
 await until(()=>document.querySelectorAll('[data-shared=detail]').length===3,'account list');
 assert(accountFixture.reads>0,'list not requested');
 const row=$('[data-shared=detail]'),reads=accountFixture.reads;row.focus();
 await until(()=>accountFixture.reads>=reads+2,'list polls');
 assert($('[data-shared=detail]')===row&&document.activeElement===row,'unchanged poll replaced row/focus');
 accountFixture.detailTruncated=true;click('[data-shared=detail]');await until(()=>document.querySelectorAll('[data-detail-tasks] button').length===2,'shared tasks');
 assert(!$('[data-detail-task-note]').hidden,'truncated detail claimed a complete list');
 accountFixture.detailTruncated=false;await until(()=>$('[data-detail-task-note]').hidden,'untruncated detail kept stale warning');
 assert(accountFixture.detailReads>0,'detail not requested');
 assert($('[data-detail-os]').textContent==='macOS','runtime OS was not projected');
 assert(JSON.stringify([...document.querySelectorAll('[data-detail-caps] .account-device-cap')].map(n=>n.textContent))===JSON.stringify(['读取文件','写入文件','终端','浏览器','电脑操作']),'backend capabilities were not projected or exec was duplicated');
 const rename=$('[data-shared=rename]');rename.focus();rename.click();
 const field=$('#account-devices-modal input');field.value='新的工作电脑';field.setSelectionRange(1,4);
 const detailReads=accountFixture.detailReads;await until(()=>accountFixture.detailReads>=detailReads+2,'detail polls');
 assert($('#account-devices-modal input')===field&&field.value==='新的工作电脑'&&field.selectionStart===1&&field.selectionEnd===4&&document.activeElement===field,'detail poll lost rename draft');
 accountFixture.failRename=true;click('[data-shared=save-device]');
 await until(()=>$('[data-shared-error]').textContent.includes('设备改名失败'),'rename failure');
 assert(field.value==='新的工作电脑'&&accountFixture.devices[0].name==='Mac mini','failure changed saved state');
 accountFixture.failRename=false;click('[data-shared=save-device]');
 await until(()=>!$('#account-devices-modal').firstChild,'rename success');
 assert(accountFixture.writes.at(-1).body.name==='新的工作电脑'&&$('[data-device-name]').textContent==='新的工作电脑','rename payload/readback');
 assert(document.activeElement===rename,'rename return focus');
 const taskNode=$('[data-detail-tasks] b'),range=document.createRange();range.selectNodeContents(taskNode);getSelection().removeAllRanges();getSelection().addRange(range);
 const selected=getSelection().toString(),poll=accountFixture.detailReads;await until(()=>accountFixture.detailReads>=poll+2,'selection polls');
 assert(getSelection().toString()===selected&&$('[data-detail-tasks] b')===taskNode,'poll erased selection');getSelection().removeAllRanges();
 const unbind=$('[data-shared=unbind]');unbind.focus();unbind.click();keyEscape();
 assert(!$('#setpage').hidden&&document.activeElement===unbind&&!accountFixture.writes.some(w=>w.method==='DELETE'),'cancel unbound or lost focus');
 click('[data-shared=unbind]');accountFixture.failDelete=true;click('[data-shared=save-device]');
 await until(()=>$('[data-shared-error]').textContent.includes('解绑失败'),'unbind failure');
 assert(accountFixture.devices.length===3,'failed unbind changed list');keyEscape();
 click('[data-shared=back]');click('[data-shared=add]');
 const before=accountFixture.reads;click('[data-shared=check-new]');
 await until(()=>accountFixture.reads>before&&!$('[data-shared=check-new]').disabled,'check actual devices');
 assert(accountFixture.devices.length===3,'check injected device');keyEscape();
 accountFixture.failList=true;await window.XBotSettings.open('devices');
 await until(()=>$('[data-shared=retry]'),'list error');accountFixture.failList=false;click('[data-shared=retry]');
 await until(()=>!$('[data-shared=retry]'),'list retry');
 click('[data-shared=detail]');await until(()=>$('[data-shared=unbind]'),'unbind detail');
 accountFixture.failDelete=false;click('[data-shared=unbind]');click('[data-shared=save-device]');
 await until(()=>document.querySelectorAll('[data-shared=detail]').length===2,'unbind result');
 assert(accountFixture.writes.at(-1).method==='DELETE','no delete request');
 accountFixture.devices=[];await window.XBotSettings.open('devices');
 assert(!$('[data-shared=detail]')&&$('[data-shared=add]'),'empty list lacks next action');
 window.XBotSettings.close();
 const frame=document.createElement('iframe');frame.style.cssText='position:fixed;left:0;top:0;width:390px;height:844px;border:0';
 const target=new URL(location.href);target.searchParams.set('account-mobile','1');frame.src=target.href;document.body.append(frame);
 await until(()=>frame.contentDocument?.body?.dataset.testResult,'mobile driver');
 assert(frame.contentDocument.body.dataset.testResult==='passed','mobile: '+frame.contentDocument.body.dataset.testResult);frame.remove();
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`

const accountSharedDeviceAvailabilityDriver = `
(async()=>{
 const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
 const until=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
 await until(()=>!$('#shell').hidden&&$('#account-device-task-status')?.textContent.includes('检查界面组件')&&!$('#account-device-task-status').hidden,'running task');
 const entry=$('#account-device-task-status'),task=accountFixture.tasks[0];
 accountFixture.tasks.push({...task,id:'t3',ref:'job/ref-3',device_id:'windows',device_name:'Windows 工作站',description:'处理另一份文档'});
 accountFixture.unavailable=[{id:'mini',name:'Mac mini',error:'device offline'}];accountFixture.tasksTruncated=true;
 await until(()=>entry.textContent.includes('部分设备状态待确认'),'unavailable metadata');
 assert(!entry.hidden&&task.status==='running','offline marked task finished or erased entry');
 entry.focus();entry.click();
 await until(()=>document.querySelectorAll('[data-current-device-tasks] [data-shared=task]').length===2,'known and reachable tasks');
 assert(!$('[data-current-task-note]').hidden,'truncated aggregate claimed complete tasks');
 accountFixture.tasksTruncated=false;await until(()=>$('[data-current-task-note]').hidden,'aggregate truncation notice did not clear');
 const old=$('[data-current-device-tasks] [data-ref="job/ref-1"]'),reachable=$('[data-current-device-tasks] [data-ref="job/ref-3"]');
 assert(old.textContent.includes('状态待确认')&&reachable.textContent.includes('正在执行'),'availability was not per device');
 old.focus();old.click();await until(()=>$('[data-shared-error]')?.textContent.includes('设备已离线'),'offline detail request');
 assert($('[data-task-meta]').textContent.includes('状态待确认')&&task.status==='running'&&!accountFixture.writes.length,'offline detail invented terminal state');
 document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));
 assert(document.activeElement===entry,'offline detail lost return focus');
 accountFixture.unavailable=[];accountFixture.tasks=accountFixture.tasks.filter(t=>t.ref!=='job/ref-3');
 await until(()=>entry.textContent.includes('检查界面组件')&&entry.textContent.includes('正在执行')&&!entry.textContent.includes('状态待确认'),'task recovery');
 assert($('#account-device-task-status')===entry&&task.status==='running','recovery replaced entry or rewrote task status');
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`

const accountSharedDevicesLocalFixture = `
const localFetch=window.fetch,localRequests=[];
let localPairing={a:{},b:{}},localAccount=null;
window.fetch=async(path,opts={})=>{
 const method=opts.method||'GET';
 if(path==='/api/account/me')return json({account:localAccount});
 if(path==='/api/account/claim'){
  localRequests.push({path,method});localAccount={...localAccount,unbound:false};return json({account:localAccount});
 }
 if(path==='/api/devices')return json({self:{name:'本机',os:'macOS',bots:bots.map(b=>b.name)},devices:[],pairing:localPairing,gateway_ok:true});
 if(path==='/api/devices/code'||path==='/api/devices/join'){
  localRequests.push({path,method});
  if(path==='/api/devices/code'&&method==='POST')localPairing={a:{phase:'ready',code:'23456789ABCD',expires:new Date(Date.now()+60000).toISOString()},b:{}};
  return json({pairing:localPairing});
 }
 return localFetch(path,opts);
};
`

const accountSharedDevicesLocalDriver = `
(async()=>{
 const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
 const until=async(fn,msg)=>{for(let i=0;i<700;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
 await until(()=>!$('#shell').hidden&&$('#title').textContent.includes(bots[0].name)&&!$('.thread-loading-cover'),'local boot');
 assert($('#account-device-foot').hidden,'account picker shown without account');
 $('#input').textContent='本机消息';$('#input').dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await until(()=>fixture.streams.length===1,'local send');assert(!('device_id' in accountFixture.sends[0]),'local message got account target');
 finish(fixture.streams[0],'本机回复');await until(()=>!$('#thread .live'),'local finish');
 await window.XBotSettings.open('devices');await until(()=>$('#g7-desktop-status')?.textContent==='无障碍已授权','local desktop status');
 $('[data-act=add]').focus();$('[data-act=add]').click();await until(()=>$('#devices-modal [data-act=join-open]'),'legacy pairing');
 await until(()=>localRequests.some(r=>r.path==='/api/devices/code'&&r.method==='POST')&&!window.xbotDevices.S.busy,'legacy code API unused');
 $('#devices-modal [data-act=join-open]').click();assert($('#g7-code'),'old join entry missing');
 $('#g7-code').dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));
 await until(()=>!$('#devices-modal').firstChild,'legacy close');
 await until(()=>localRequests.some(r=>r.path==='/api/devices/code'&&r.method==='DELETE'),'pair code cleanup');
 assert(!$('#setpage').hidden&&accountFixture.reads===0,'legacy changed account pool or closed settings');
 localAccount={email:'local@example.test',owner:true,unbound:true};await window.xbotDevices.reloadAccount();
 assert(!$('#account-device-foot').hidden,'account login did not reveal picker');
 await window.XBotSettings.open('devices');await until(()=>$('[data-act=claim]'),'explicit local bind absent');
 const claims=()=>localRequests.filter(r=>r.path==='/api/account/claim');assert(!claims().length,'login silently bound computer');
 $('[data-act=claim]').click();await until(()=>!$('[data-act=claim]')&&!window.xbotDevices.S.claimBusy,'claim response');
 assert(claims().length===1&&claims()[0].method==='POST','claim was not requested');
 window.xbotDevices.signedOut();assert($('#account-device-foot').hidden,'account logout left picker');
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`

const accountSharedDeviceTargetDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const until=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const click=s=>{const n=$(s);assert(n&&n.getClientRects().length,'missing '+s);n.focus();n.click()};
const escape=()=>document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));
const draft=text=>{$('#input').textContent=text;$('#input').dispatchEvent(new Event('input'))};
const choose=async id=>{click('#account-device-trigger');await until(()=>$('[data-shared=choose][data-id="'+id+'"]'),'picker');click('[data-shared=choose][data-id="'+id+'"]')};
const select=async name=>{click('#roster [data-bot="'+name+'"]');await until(()=>$('#title').textContent.includes(name)&&!$('.thread-loading-cover'),'switch '+name)};
(async()=>{
 await until(()=>!$('#shell').hidden&&!$('.thread-loading-cover')&&!$('#account-device-foot').hidden,'boot');
 const bot=bots[0].name,other=bots[1].name;
 click('#account-device-trigger');await until(()=>$('[data-shared=choose][data-id=offline]'),'picker load');
 assert($('[data-shared=choose][data-id=offline]').disabled,'offline selectable');
 const search=$('#account-device-search');search.value='macOS';search.dispatchEvent(new Event('input',{bubbles:true}));
 assert($('[data-shared=choose][data-id=mini]')&&$('[data-shared=choose][data-id=offline]')&&!$('[data-shared=choose][data-id=windows]'),'search ignored displayed runtime OS name');
 search.value='missing';search.dispatchEvent(new Event('input',{bubbles:true}));
 assert(!$('[data-shared=choose]')&&$('[data-shared=clear-search]'),'no match');click('[data-shared=clear-search]');
 const reads=accountFixture.reads;search.value='Windows';search.dispatchEvent(new Event('input',{bubbles:true}));search.setSelectionRange(1,4);
 await until(()=>accountFixture.reads>=reads+2,'picker polling');
 assert($('#account-device-search')===search&&search.value==='Windows'&&search.selectionStart===1&&search.selectionEnd===4&&document.activeElement===search,'poll lost search');
 escape();assert(document.activeElement===$('#account-device-trigger'),'picker focus return');
 await choose('windows');draft('Bot A 草稿');await select(other);await choose('mini');draft('Bot B 草稿');await select(bot);
 assert($('#input').textContent==='Bot A 草稿'&&window.xbotDevices.targetFor(bot)==='windows','A draft/target lost');
 await select(other);assert($('#input').textContent==='Bot B 草稿'&&window.xbotDevices.targetFor(other)==='mini','B draft/target lost');await select(bot);
 accountFixture.failSend=true;$('#composer').requestSubmit();await until(()=>$('#input').textContent==='Bot A 草稿'&&$('#thread .send-failed'),'send failed');
 assert(accountFixture.sends.at(-1).device_id==='windows'&&window.xbotDevices.targetFor(bot)==='windows','failed send lost target');
 assert($('#thread .send-failed').textContent.includes('目标设备已离线'),'send failure hid backend reason');
 accountFixture.failSend=false;$('#composer').requestSubmit();await until(()=>fixture.streams.length===1&&window.xbotDevices.targetFor(bot)==='','send accepted');
 assert(accountFixture.sends.at(-1).device_id==='windows','accepted send lost frozen target');
 const running=accountFixture.tasks[0];assert(running.device_id==='mini','picker rewrote running task');
 finish(fixture.streams[0],'实际消息回复');await until(()=>!$('#thread .live'),'finish');
 await until(()=>!$('#account-device-task-status').hidden,'actual task status');
 click('#account-device-task-status');await until(()=>$('[data-task-output]').textContent.includes('实际执行输出'),'task output');
 assert($('[data-task-meta]').textContent.includes('Mac mini'),'task device not actual');
 accountFixture.failStop=true;click('[data-shared=stop-task]');await until(()=>$('[data-shared-error]').textContent.includes('停止失败'),'stop failure');
 assert(running.status==='running'&&!$('[data-shared=stop-task]').hidden,'failure faked cancellation');
 accountFixture.failStop=false;click('[data-shared=stop-task]');await until(()=>$('[data-shared=stop-task]').hidden,'stop actual response');
 assert(running.status==='cancelled'&&accountFixture.writes.some(w=>w.ref===running.ref&&w.method==='POST'),'stop not requested');
 running.status='completed';running.artifacts=[{id:'a1',name:'真实产物.txt',size:40000}];
 click('[data-shared=refresh-task]');await until(()=>$('[data-shared=artifact]'),'artifact');click('[data-shared=artifact]');
 await until(()=>accountFixture.artifactReads.length===2&&!$('[data-shared=artifact]').disabled,'artifact chunks');
 assert(accountFixture.artifactReads[0].offset===0&&accountFixture.artifactReads[1].offset===32768&&accountFixture.artifactReads.every(r=>r.limit<=32768),'unbounded or incomplete artifact');
 escape();assert(document.activeElement===$('#account-device-task-status'),'task focus return');
 await choose('windows');await window.XBotSettings.open('devices');click('[data-shared=detail][data-id=windows]');
 await until(()=>$('[data-shared=unbind]'),'windows detail');click('[data-shared=unbind]');click('[data-shared=save-device]');
 await until(()=>!$('[data-shared=detail][data-id=windows]')&&!$('#account-devices-modal').firstChild&&window.xbotDevices.targetFor(bot)==='','unbind chosen');
 assert(window.xbotDevices.targetFor(bot)==='','removed target retained');
 assert(window.xbotDevices.targetFor(other)==='mini','unrelated bot target changed');
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`
