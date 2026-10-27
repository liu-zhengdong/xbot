package web

import "testing"

// Exercise the real menu and settings navigation with scripted HTTP.
func TestAccountMenuBrowser(t *testing.T) {
	runPageInChrome(t, `
(async()=>{
 const $=id=>document.getElementById(id), assert=(v,m)=>{if(!v)throw Error(m)};
 const until=async(f,m)=>{for(let i=0;i<200;i++){if(f())return;await new Promise(r=>setTimeout(r,10))}throw Error(m)};
 await until(()=>!$('shell').hidden,'app did not open');
 window.innerWidth=1440;window.dispatchEvent(new Event('resize'));
 const check=()=>{
  $('account').click();assert(!$('account-menu').hidden&&$('account').getAttribute('aria-expanded')==='true','menu did not open');
  const buttons=[...$('account-menu').querySelectorAll('button')].filter(b=>b.getClientRects().length);
  assert(buttons.length===1&&buttons[0].id==='account-settings'&&buttons[0].querySelector('svg'),'menu must contain only settings with icon');
  $('account').click();assert($('account-menu').hidden&&$('account').getAttribute('aria-expanded')==='false','menu did not close');
 };
 check();
 const grip=$('side-grip');grip.setPointerCapture=()=>{};
 grip.dispatchEvent(new PointerEvent('pointerdown',{pointerId:1,button:0,bubbles:true}));
 grip.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,clientX:$('sidebar').getBoundingClientRect().left+50,bubbles:true}));
 grip.dispatchEvent(new PointerEvent('pointerup',{pointerId:1,bubbles:true}));
 assert($('sidebar').classList.contains('rail'),'drag did not open rail');check();
 $('account').click();$('account-settings').click();
 assert($('account-menu').hidden&&!$('setpage').hidden,'settings did not open and close menu');
 assert(!document.querySelector('[data-spane=general]').hidden,'settings did not open general');
 for(const section of ['devices','about']){
  document.querySelector('[data-set='+section+']').click();
  assert(!document.querySelector('[data-spane='+section+']').hidden,section+' settings unavailable');
 }
 document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
 assert($('setpage').hidden,'settings did not close');check();
 assert(!testErrors.length,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
