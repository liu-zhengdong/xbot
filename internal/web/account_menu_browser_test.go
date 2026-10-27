package web

import "testing"

func TestAccountMenuBrowser(t *testing.T) {
	runPageInChrome(t, `
(async()=>{
 const $=id=>document.getElementById(id), assert=(v,m)=>{if(!v)throw Error(m)};
 const until=async(f,m)=>{for(let i=0;i<200;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
 await until(()=>!$('shell').hidden,'app did not open');
 const base=window.fetch; let account=null, cloudCalls=0;
 window.fetch=async(p,o)=>{
  if(p==='/api/account/me')return json({account});
  if(p==='/api/account/devices'){cloudCalls++;return json({devices:[]})}
  if(p==='/api/account/login'||p==='/api/account/register'){account={email:'person@example.com',official:true,unbound:true};return json({account})}
  if(p==='/api/account/logout'){account=null;return json({status:'ok'})}
  if(p==='/api/account/claim'){account={...account,unbound:false,owner:true};return json({account})}
  return base(p,o);
 };
 $('account').click();
 assert([...$('account-menu').querySelectorAll('button')].filter(b=>!b.hidden).map(b=>b.textContent.trim()).join()==='设置','menu should contain only settings');
 $('account-settings').click(); document.querySelector('[data-set=account]').click();
 await until(()=>$('acct-login-email'),'account login missing');
 $('acct-login-email').value='person@example.com';$('acct-login-pass').value='password';
 $('acct-login-email').closest('form').requestSubmit();
 await until(()=>$('settings-account').textContent.includes('p***@example.com'),'signed-in account missing');
 document.querySelector('[data-set=devices]').click();
 await until(()=>$('settings-devices').textContent.includes('通过账号关联设备'),'claim missing');
 assert($('settings-devices').textContent.includes('已匹配设备'),'matched devices missing');
 $('settings-devices').querySelector('[data-act=claim]').click();
 await until(()=>!$('settings-devices').querySelector('[data-act=claim]'),'claim not completed');
 document.querySelector('[data-set=account]').click();
 await until(()=>$('settings-account').textContent.includes('退出登录'),'logout missing');
 [...$('settings-account').querySelectorAll('button')].find(b=>b.textContent==='退出登录').click();
 await until(()=>$('acct-login-email'),'logout did not show login');
 assert(!$('shell').hidden,'logout closed local shell');
 [...$('settings-account').querySelectorAll('button')].find(b=>b.textContent==='注册').click();
 $('acct-reg-email').value='person@example.com';$('acct-reg-pass').value='password';$('acct-reg-pass2').value='password';
 $('acct-reg-email').closest('form').requestSubmit();
 await until(()=>$('settings-account').textContent.includes('p***@example.com'),'registration did not sign in');
 [...$('settings-account').querySelectorAll('button')].find(b=>b.textContent==='退出登录').click();
 await until(()=>$('acct-login-email'),'second logout failed');
 document.querySelector('[data-set=devices]').click();
 await until(()=>$('settings-devices').textContent.includes('已匹配设备'),'device list missing');
 assert(!$('settings-devices').textContent.includes('通过账号关联设备'),'logged-out claim visible');
 assert(cloudCalls===0,'local UI fetched cloud device list');
 assert(!testErrors.length,testErrors.join('; ')); document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
