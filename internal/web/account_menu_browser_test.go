package web

import "testing"

func TestAccountMenuBrowser(t *testing.T) {
	runPageInChrome(t, `
(async()=>{
 const $=id=>document.getElementById(id), assert=(v,m)=>{if(!v)throw Error(m)};
 const heads=()=>[...$('settings-devices').querySelectorAll('.g7-body > section')].map(s=>s.id==='g7-claim'?'claim':s.querySelector('h3').textContent.replace(/ · \d+$/,'')).join();
 const until=async(f,m)=>{for(let i=0;i<200;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
 await until(()=>!$('shell').hidden,'app did not open');
 const base=window.fetch; let account=null, accountDeviceCalls=0, externalCalls=0;
 window.fetch=async(p,o)=>{
  if(p==='/api/account/me')return json({account});
  if(typeof p==='string' && /^https?:\/\//.test(p) && !p.startsWith(location.origin)) externalCalls++;
  if(p==='/api/account/devices'){accountDeviceCalls++;return json({devices:[{id:'other',name:'另一台电脑',online:true,self:false,bots:[]}]})}
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
 // 已登未绑：这台电脑、关联提示、已匹配设备三段，不另列账号设备，也没有底部「连接」。
 await until(()=>$('g7-claim'),'claim missing');
 assert(heads()==='这台电脑,claim,已匹配设备',heads());
 assert($('g7-claim').textContent.includes('通过账号关联设备')&&$('g7-claim').textContent.includes('把这台电脑绑到账号，手机和其它入口也能看到这台上的 Bot。'),'claim copy');
 assert($('g7-claim').querySelector('[data-act=claim]').textContent.trim()==='绑定这台电脑','claim CTA');
 assert(!$('g7-account-devices')&&![...$('settings-devices').querySelectorAll('h3')].some(h=>h.textContent==='账号关联设备'),'account device list rendered');
 assert(!$('settings-devices').querySelector('.g7-bound'),'unbound shows bound badge');
 $('settings-devices').querySelector('[data-act=claim]').click();
 // 已登已绑：关联提示整节不渲染，本机卡弱标「已绑到账号」。
 await until(()=>!$('g7-claim'),'claim not completed');
 assert(heads()==='这台电脑,已匹配设备',heads());
 assert($('settings-devices').querySelector('.g7-self .g7-bound').textContent==='已绑到账号','bound badge missing');
 assert(!$('settings-devices').querySelector('[data-act=claim]'),'bound still offers claim');
 assert(accountDeviceCalls===0,'local devices page fetched account devices');
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
 const callsBeforeLoggedOut=accountDeviceCalls;
 document.querySelector('[data-set=devices]').click();
 await until(()=>$('settings-devices').textContent.includes('已匹配设备'),'device list missing');
 assert(heads()==='这台电脑,已匹配设备',heads());
 assert(!$('settings-devices').textContent.includes('账号'),'logged-out account UI visible');
 assert(accountDeviceCalls===callsBeforeLoggedOut,'logged-out local UI fetched account devices');
 assert(externalCalls===0,'local UI opened an external domain');
 assert(!testErrors.length,testErrors.join('; ')); document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
