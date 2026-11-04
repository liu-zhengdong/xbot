package web

import "testing"

// Scroll the real settings pages, not a CSS/text snapshot: only the inset
// content column should move, while navigation and page title stay put.
func TestSettingsContentScrollBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture, `
(async()=>{
 const $=id=>document.getElementById(id), assert=(v,m)=>{if(!v)throw Error(m)};
 const until=async(f,m)=>{for(let i=0;i<200;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
 const frame=()=>new Promise(r=>setTimeout(r,50));
 await until(()=>!$('shell').hidden,'shell missing');
 const base=window.fetch;
 window.fetch=async(p,o)=>p==='/api/apps'?json({apps:['Playwright','Context7','Telegram','飞书','Slack','Discord','GitHub','Notion','Linear','Jira','Figma','Sentry'].map((title,i)=>({id:'app-'+i,title,summary:'连接 '+title+'，让 Bot 帮你处理工作。',category:'dev',state:'available'})),connected:0}):base(p,o);
 $('account').click();$('account-settings').click();
 const rect=n=>n.getBoundingClientRect(), cnt=document.querySelector('.setcnt');
 for(const section of ['apps','general']){
  document.querySelector('[data-set="'+section+'"]').click();
  if(section==='apps')await until(()=>document.querySelector('.ap-head'),'apps missing');
  const pane=document.querySelector('[data-spane="'+section+'"]');
  const title=pane.querySelector(section==='apps'?'.ap-head':'h2');
  const filler=document.createElement('div');filler.style.height='1800px';title.parentElement.append(filler);
  cnt.scrollTop=0;await frame();
  const nav=document.querySelector('.setnav');
  const before=[nav,title].map(n=>rect(n).top), contentBefore=rect(filler).top;
  cnt.scrollTop=300;await frame();
  assert(cnt.scrollTop===300,section+': content cannot scroll');
  assert(rect(filler).top<contentBefore-250,section+': body did not move');
  [nav,title].forEach((n,i)=>assert(Math.abs(rect(n).top-before[i])<1,section+': shell/title moved '+i));
  const main=rect(document.querySelector('.setmain')), area=rect(cnt);
  assert(area.right<main.right && area.top>main.top && area.bottom<main.bottom,section+': scrollbar not inset');
  assert($('setpage').scrollTop===0 && nav.scrollTop===0,section+': outer shell scrolled');
  filler.remove();
 }
 document.querySelector('[data-set="apps"]').click();
 await until(()=>document.querySelector('.ap-head'),'apps did not reopen');
 cnt.scrollTop=Number(new URLSearchParams(location.search).get('scroll')||0);
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
