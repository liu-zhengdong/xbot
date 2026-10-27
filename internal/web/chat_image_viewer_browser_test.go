package web

import "testing"

// The shipped UI, with only HTTP/data scripted. Gestures using trusted browser
// input are exercised by the companion local Playwright evidence run.
func TestChatImageViewerBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+chatImageViewerFixture, chatImageViewerDriver)
}

const chatImageViewerFixture = `
const imageIDs=Array.from({length:4},(_,i)=>String(i+1).repeat(32)+'.png');
fixture.messages['2号小伙子']=[
 ...Array.from({length:25},(_,i)=>message(i%2?'assistant':'user','这是一条聊天记录 '+i)),
 {...message('assistant','这几张是同一条消息里的图片。'),images:imageIDs},
 {...message('user','另一条消息的图片。'),images:['5'.repeat(32)+'.png']},
 {...message('assistant','这张图片已失效。'),images:['6'.repeat(32)+'.png']}
];
const imageSrc=Object.getOwnPropertyDescriptor(HTMLImageElement.prototype,'src');
Object.defineProperty(HTMLImageElement.prototype,'src',{...imageSrc,set(value){
 if(String(value).includes('/paste/')){
  this.dataset.deliveryUrl=value;
  const n=Number(String(value).split('/').at(-1)[0]);
  const canvas=document.createElement('canvas');canvas.width=n===2?900:1600;canvas.height=n===2?1400:1000;
  const ctx=canvas.getContext('2d');ctx.fillStyle=['','#cad7d1','#d6c6b6','#b8cbdc','#c9c1d2','#e1d6ad'][n]||'#ddd';ctx.fillRect(0,0,canvas.width,canvas.height);
  ctx.fillStyle='#303b39';ctx.font='64px sans-serif';ctx.fillText('IMAGE '+n,80,120);
  ctx.beginPath();ctx.arc(canvas.width/2,canvas.height/2,220,0,Math.PI*2);ctx.fill();
  value=n===6?'data:image/png;base64,broken':canvas.toDataURL();
 }
 imageSrc.set.call(this,value);
}});
`

const chatImageViewerDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const key=k=>document.dispatchEvent(new KeyboardEvent('keydown',{key:k,bubbles:true,cancelable:true}));
const view=()=>$('#lightbox'),picture=()=>$('#lightbox img');
const current=n=>picture()?.dataset.deliveryUrl.endsWith('/'+String(n).repeat(32)+'.png');
const loaded=()=>picture()?.naturalWidth>0&&!picture().hidden;
const matrix=()=>new DOMMatrix(getComputedStyle(picture()).transform);
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('[data-bot="2号小伙子"]'),'shell missing');
 if(innerWidth<768)$('[data-bot="2号小伙子"]').click();
 await waitFor(()=>$('#thread .gallery img')?.naturalWidth>0,'history missing');
 const thumbs=[...document.querySelectorAll('#thread .gallery .thumb')];
 const thread=$('#thread');thread.scrollTop=thread.scrollHeight;
 if(new URLSearchParams(location.search).has('manual')){document.body.dataset.testResult='ready';return}
 const before=thread.scrollTop;
 thumbs[1].focus({preventScroll:true});thumbs[1].click();
 await waitFor(loaded,'viewer did not load');
 assert(view().querySelectorAll('img').length===1,'viewer is still a multi-image wall');
 assert(current(2),'clicked image index lost');
 assert(view().matches(':modal')&&view().getAttribute('aria-label'),'viewer does not isolate background');
 assert($('.image-count').textContent==='2 / 4','wrong message count');
 assert(view().contains(document.activeElement),'focus stayed behind viewer');
 key('ArrowRight');await waitFor(()=>current(3)&&loaded(),'right key failed');
 $('[aria-label="下一张"]').click();await waitFor(()=>current(4)&&loaded(),'next button failed');
 key('ArrowRight');assert(current(4),'crossed message boundary at end');
 key('ArrowLeft');key('ArrowLeft');key('ArrowLeft');await waitFor(()=>current(1)&&loaded(),'previous failed');
 $('[aria-label="上一张"]').click();assert(current(1),'wrapped at beginning');
 const r=picture().getBoundingClientRect(),cx=r.x+r.width/2,cy=r.y+r.height/2;
 picture().dispatchEvent(new MouseEvent('dblclick',{bubbles:true,clientX:cx,clientY:cy}));
 await waitFor(()=>matrix().a>1,'double click did not zoom');
 key('ArrowRight');await waitFor(()=>current(2)&&loaded()&&matrix().a===1,'switch did not reset fit');
 picture().dispatchEvent(new WheelEvent('wheel',{bubbles:true,cancelable:true,deltaY:-150,clientX:cx,clientY:cy}));
 await waitFor(()=>matrix().a>1,'wheel did not zoom');
 key('Tab');assert(view().contains(document.activeElement),'Tab escaped');
 key('Escape');assert(view().hidden&&!view().open,'Escape did not close');
 assert(document.activeElement===thumbs[1]&&Math.abs(thread.scrollTop-before)<1,'focus or chat scroll lost');
 thumbs[3].click();await waitFor(loaded,'single image missing');
 assert(current(5)&&$('.image-count').hidden,'single image shows count or wrong source');
 assert($('[aria-label="上一张"]').hidden&&$('[aria-label="下一张"]').hidden,'single has navigation');
 key('ArrowLeft');key('ArrowRight');assert(current(5),'single image crossed message');
 $('[aria-label="关闭"]').click();assert(view().hidden,'close button failed');
 thumbs[4].click();await waitFor(()=>$('.image-status')?.textContent==='图片暂时无法显示','broken image state missing');
 key('Escape');
 thumbs[2].click();await waitFor(loaded,'third image missing');assert(current(3),'plus thumbnail opened first image');
 view().dispatchEvent(new MouseEvent('click',{bubbles:true,clientX:1,clientY:1}));assert(view().hidden,'blank mask did not close');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
