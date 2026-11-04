package web

import "testing"

// Use the settings entrypoint and native radio events with scripted push services.
func TestSettingsGeneralBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+settingsGeneralFixture, settingsGeneralDriver)
}

const settingsGeneralFixture = `
const generalFixture={subscribed:true,permission:'default',permissionAnswer:'granted',permissionRequests:0,subscriptions:0,methods:[],failMethod:''};
Object.defineProperty(window,'Notification',{configurable:true,value:{
 get permission(){return generalFixture.permission},
 async requestPermission(){generalFixture.permissionRequests++;return generalFixture.permission=generalFixture.permissionAnswer}
}});
Object.defineProperty(window,'PushManager',{configurable:true,value:function(){}});
const generalRegistration={pushManager:{async subscribe(options){
 if(!options.userVisibleOnly||!(options.applicationServerKey instanceof Uint8Array))throw Error('invalid subscribe options');
 generalFixture.subscriptions++;
 return {toJSON:()=>({endpoint:'https://push.example.test/subscription',keys:{p256dh:'fixture-public',auth:'fixture-auth'}})};
}}};
Object.defineProperty(navigator,'serviceWorker',{configurable:true,value:{
 ready:Promise.resolve(generalRegistration),register:async()=>generalRegistration
}});
const generalFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(path==='/api/push')return json({subscribed:generalFixture.subscribed,public_key:'AQ'});
 if(path==='/api/push/subscribe'){
  const method=opts.method||'GET';generalFixture.methods.push(method);
  if(generalFixture.failMethod===method)return new Response(JSON.stringify({error:'scripted push failure'}),{status:503,headers:{'Content-Type':'application/json'}});
  if(method==='POST'){
   const body=JSON.parse(opts.body);
   if(body.endpoint!=='https://push.example.test/subscription'||!body.p256dh||!body.auth)throw Error('subscription payload missing');
   generalFixture.subscribed=true;
  }else if(method==='DELETE')generalFixture.subscribed=false;
  else throw Error('unexpected subscription method');
  return json({ok:true});
 }
 return generalFetch(path,opts);
};
`

const settingsGeneralDriver = `
(async()=>{
 const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
 const until=async(fn,msg)=>{for(let i=0;i<500;i++){if(fn())return;await new Promise(resolve=>setTimeout(resolve,10))}throw Error(msg)};
 const visible=node=>!!node&&node.getClientRects().length>0;
 const click=selector=>{const node=$(selector);assert(visible(node),'control not visible: '+selector);node.click()};
 const open=()=>{click('#account');click('#account-settings')};
 await until(()=>!$('#shell').hidden,'shell did not open');
 if(new URLSearchParams(location.search).has('settings-mobile')){
  assert(matchMedia('(max-width: 640px)').matches,'iframe is not a phone viewport');
  click('#menu');open();
  await until(()=>visible($('.setnav'))&&!visible($('.setmain')),'phone settings directory missing');
  assert($('#shell').inert,'settings did not isolate the shell');
  click('[data-set="general"]');
  await until(()=>visible($('#settings-theme'))&&!$('#phone-push').disabled,'phone general did not open');
  assert(!visible($('.setnav'))&&visible($('#settings-back')),'phone general navigation missing');
  click('#settings-back');
  await until(()=>visible($('.setnav'))&&!visible($('.setmain')),'back did not restore directory');
  click('[data-set="perm"]');
  await until(()=>visible($('#settings-permissions'))&&!$('#settings-permissions').disabled,'other settings did not open');
  assert(!visible($('#settings-theme'))&&!visible($('.setnav')),'general stayed visible over other settings');
  click('#settings-close-m');
  await until(()=>$('#setpage').hidden&&$('#setscrim').hidden&&!$('#shell').inert,'phone close did not restore shell');
 }else{
  open();
  await until(()=>visible($('#settings-theme'))&&!$('#phone-push').disabled,'general did not open');
  const theme=value=>$('#settings-theme input[value="'+value+'"]');
  for(const mode of ['dark','light','system']){
   click('#settings-theme label.theme-option-'+mode);
   await until(()=>theme(mode).checked&&localStorage.getItem('xbot-theme')===mode,'theme preference not saved: '+mode);
   const resolved=mode==='system'?(matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'):mode;
   assert(document.documentElement.dataset.theme===resolved,'theme not applied: '+mode);
   assert($('#settings-theme').querySelectorAll('input:checked').length===1,'multiple themes selected');
  }
  const selected=value=>$('#phone-push input[value="'+value+'"]').checked;
  // Labels activate the native radio, which emits the production change event.
  const pushLabel=value=>$('#phone-push input[value="'+value+'"]').closest('label');
  const pushClick=async(value,method,subscribed)=>{
   const before=generalFixture.methods.length;
   assert(visible(pushLabel(value)),'push option hidden');pushLabel(value).click();
   await until(()=>generalFixture.methods.length===before+1&&!$('#phone-push').disabled,'push request did not settle: '+method);
   assert(generalFixture.methods.at(-1)===method,'wrong push method');
   assert(generalFixture.subscribed===subscribed&&selected(subscribed?'on':'off'),'push selection disagrees with saved state');
  };
  assert(selected('on'),'initial saved subscription missing');
  await pushClick('off','DELETE',false);
  await pushClick('on','POST',true);
  assert(generalFixture.permissionRequests===1&&generalFixture.subscriptions===1,'enable skipped browser permission or subscription');
  const normalHint=$('#phone-push-hint').textContent;
  generalFixture.failMethod='DELETE';await pushClick('off','DELETE',true);
  assert(visible($('#phone-push-hint'))&&$('#phone-push-hint').textContent!==normalHint,'delete failure was silent');
  generalFixture.failMethod='';await pushClick('off','DELETE',false);
  generalFixture.permission='default';generalFixture.permissionAnswer='denied';
  const beforeDenied=generalFixture.methods.length;
  pushLabel('on').click();
  await until(()=>generalFixture.permissionRequests===2&&!$('#phone-push').disabled,'permission refusal did not settle');
  assert(selected('off')&&!generalFixture.subscribed,'permission refusal changed saved selection');
  assert(generalFixture.methods.length===beforeDenied&&generalFixture.subscriptions===1,'denied permission reached push subscription');
  assert(visible($('#phone-push-hint'))&&$('#phone-push-hint').textContent!==normalHint,'permission refusal was silent');
  generalFixture.permission='granted';generalFixture.failMethod='POST';
  await pushClick('on','POST',false);
  assert(visible($('#phone-push-hint'))&&$('#phone-push-hint').textContent!==normalHint,'subscribe failure was silent');
  click('#settings-close');open();
  await until(()=>!$('#phone-push').disabled&&visible($('#settings-theme')),'general did not reopen');
  assert(theme('system').checked&&localStorage.getItem('xbot-theme')==='system','reopen lost theme preference');
  assert(selected('off'),'reopen lost saved push state');
   const lastOption=$('#phone-push input[value="off"]');lastOption.focus();
   const tab=new KeyboardEvent('keydown',{key:'Tab',bubbles:true,cancelable:true});lastOption.dispatchEvent(tab);
   assert(tab.defaultPrevented&&document.activeElement===$('[data-set="general"]'),'Tab escaped the selected radio at the end of settings');
  click('#settings-close');
  const mobile=document.createElement('iframe');
  mobile.style.cssText='position:fixed;left:0;top:0;width:390px;height:844px;border:0';
  const target=new URL(location.href);target.searchParams.set('settings-mobile','1');mobile.src=target.href;document.body.append(mobile);
  await until(()=>mobile.contentDocument?.body?.dataset.testResult,'phone driver did not finish');
  assert(mobile.contentDocument.body.dataset.testResult==='passed','phone driver: '+mobile.contentDocument.body.dataset.testResult);
  mobile.remove();
 }
 assert(!fixture.streams.length,'settings started a model request');
 assert(!testErrors.length,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(error=>{document.body.dataset.testResult='failed: '+error.message});
`
