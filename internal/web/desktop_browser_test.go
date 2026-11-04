package web

import "testing"

func TestDesktopStatusBrowser(t *testing.T) {
	runPageInChrome(t, `
(async()=>{
 const $=s=>document.querySelector(s), assert=(v,m)=>{if(!v)throw Error(m)};
 const until=async(f,m)=>{for(let i=0;i<300;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
 const base=window.fetch;let state='denied',calls=0,pending=null;
 const selfDevice={id:'self',name:'这台电脑',self:true,online:true,os:'macOS',capabilities:['desktop'],bots:[],running_count:0};
 window.fetch=async(p,o)=>{
  if(p==='/api/account/devices')return json({devices:[selfDevice]});
  if(p==='/api/account/devices/self')return json({device:selfDevice,tasks:[]});
  if(p==='/api/devices/desktop'){
   calls++;
   if(state==='pending')return new Promise(resolve=>{pending=resolve});
   if(state==='missing')return json({platform:'darwin',error:'desktop: agent-desktop is not installed'});
   if(state==='failed')throw Error('offline');
   if(state==='windows')return json({platform:'windows',status:{ok:true,data:{permissions:{automation:{state:'not_required'}}}}});
   return json({platform:'darwin',status:{ok:true,data:{permissions:{accessibility:{state}}}}});
  }
  return base(p,o);
 };
 await window.XBotSettings.open('devices');
 await until(()=>$('[data-shared="detail"][data-id="self"]'),'self device hidden');
 $('[data-shared="detail"][data-id="self"]').click();
 await until(()=>$('#g7-desktop-status')?.textContent==='无障碍未授权','denied hidden');
 assert($('#g7-desktop-help').textContent.includes('系统设置'),'no manual guide');
 const button=()=>$('#settings-devices [data-act="desktop-check"]');
 const check=async(s,expected)=>{state=s;button().focus();button().click();await until(()=>$('#g7-desktop-status').textContent===expected&&!button().disabled,'status '+s);assert(document.activeElement===button(),'recheck stole focus')};
 await check('granted','无障碍已授权');
 await check('windows','使用 Windows 系统授权');
 assert($('#g7-desktop-help').textContent.includes('权限比 XBot 更高'),'no integrity explanation');
 await check('missing','暂时查不到状态');
 assert($('#g7-desktop-help').textContent.includes('npm install -g agent-desktop'),'no install route');
 await check('failed','暂时查不到状态');
 assert($('#g7-desktop-help').textContent.includes('失败'),'failure disguised');
 state='pending';button().click();await until(()=>pending,'request not pending');
 await window.XBotSettings.open('general');
 pending(json({platform:'darwin',status:{ok:true,data:{permissions:{accessibility:{state:'granted'}}}}}));
 await new Promise(r=>setTimeout(r,10));
 assert(!$('#g7-desktop-status'),'late status reopened devices');
 assert(calls===6,'unexpected automatic polling '+calls);
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed';document.body.dataset.testError=e.stack||String(e)});
`)
}
