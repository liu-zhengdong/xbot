package web

import "testing"

// Settings · account fills .setcnt: profile card and logout must not stay
// capped at the account shell's 420/560px when the content area is wider.
func TestAccountSettingsFullWidthBrowser(t *testing.T) {
	runPageInChrome(t, `
(async()=>{
 const $=id=>document.getElementById(id), assert=(v,m)=>{if(!v)throw Error(m)};
 const until=async(f,m)=>{for(let i=0;i<200;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
 await until(()=>!$('shell').hidden,'app did not open');
 const base=window.fetch;
 window.fetch=async(p,o)=>p==='/api/account/me'?json({account:{email:'person@example.com',official:true,owner:true}}):base(p,o);
 $('account').click();$('account-settings').click();document.querySelector('[data-set=account]').click();
 await until(()=>document.querySelector('.ac-settings-profile'),'profile missing');
 // widen the content area past the old 420/560 caps; viewport stays >640 so the wide-stage media rules apply
 const c=$('settings-account').closest('.setcnt');c.style.flex='none';c.style.width='900px';
 const w=n=>n.getBoundingClientRect().width, cs=getComputedStyle(c);
 const avail=c.clientWidth-parseFloat(cs.paddingLeft)-parseFloat(cs.paddingRight);
 const prof=document.querySelector('.ac-settings-profile'), out=[...$('settings-account').querySelectorAll('button')].find(b=>b.textContent==='退出登录');
 const got='vw='+innerWidth+' avail='+avail+' pane='+w($('settings-account'))+' profile='+w(prof)+' logout='+(out&&w(out));
 assert(innerWidth>640,'viewport too narrow for wide-stage rules: '+got);
 assert(avail>560,'content area not wide enough: '+got);
 assert(out,'logout missing: '+got);
 for(const [name,n] of [['pane',$('settings-account')],['profile',prof],['logout',out]])assert(Math.abs(w(n)-avail)<=4,name+' not full width: '+got);
 assert(!testErrors.length,testErrors.join('; ')); document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
