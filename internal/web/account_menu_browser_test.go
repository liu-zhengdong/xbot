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
 const edges=m=>{const a=$('account').getBoundingClientRect(),b=$('account-menu').getBoundingClientRect();assert(Math.abs(a.left-b.left)<1&&Math.abs(a.right-b.right)<1,m+' menu '+b.left+'-'+b.right+' vs 我 '+a.left+'-'+a.right)};
 edges('expanded');
 $('account').click(); const side=document.querySelector('.sidebar'); side.classList.add('rail'); side.style.setProperty('--side-w','72px');
 $('account').click(); edges('rail');
 assert($('account-settings').getBoundingClientRect().right<=$('account-menu').getBoundingClientRect().right,'rail settings row overflows');
 side.classList.remove('rail'); side.style.removeProperty('--side-w');
 $('account-settings').click(); document.querySelector('[data-set=account]').click();
 await until(()=>$('acct-login-email'),'account login missing');
 $('acct-login-email').value='person@example.com';$('acct-login-pass').value='password';
 $('acct-login-email').closest('form').requestSubmit();
 await until(()=>$('settings-account').textContent.includes('p***@example.com'),'signed-in account missing');
 document.querySelector('[data-set=devices]').click();
 // 已登未绑：账号设备页顶上给「绑定这台电脑」，下面列账号里的设备。
 await until(()=>$('settings-devices').querySelector('[data-account-claim] [data-act=claim]'),'claim missing');
 const claim=$('settings-devices').querySelector('[data-account-claim]');
 assert(claim.textContent.includes('这台电脑尚未绑定。绑定后，账号内所有 Bot 均可使用。'),'claim copy');
 assert(claim.querySelector('[data-act=claim]').textContent.trim()==='绑定这台电脑','claim CTA');
 await until(()=>$('settings-devices').querySelectorAll('.account-device-row').length===1,'account device row missing');
 assert($('settings-devices').textContent.includes('另一台电脑'),'account device name');
 assert(!$('settings-devices').querySelector('.g7-bound'),'unbound shows bound badge');
 assert(accountDeviceCalls>0,'logged-in devices page never read account devices');
 $('settings-devices').querySelector('[data-act=claim]').click();
 // 绑完这提示就撤；短码配对页里本机卡弱标「已绑到账号」。
 await until(()=>!$('settings-devices').querySelector('[data-account-claim] [data-act=claim]'),'claim not completed');
 $('settings-devices').querySelector('[data-shared=legacy]').click();
 await until(()=>$('settings-devices').querySelector('.g7-self .g7-bound'),'bound badge missing');
 assert($('settings-devices').querySelector('.g7-self .g7-bound').textContent==='已绑到账号','bound badge text');
 assert(heads()==='这台电脑,已匹配设备',heads());
 assert(!$('settings-devices').querySelector('[data-act=claim]'),'bound still offers claim');
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
