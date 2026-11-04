package web

import "testing"

func TestAsideSourceBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+asideFixture, asideDriver)
}

func TestAsideDisclosureStableBrowser(t *testing.T) {
	runChatFramesBrowser(t, asideFixture+asideScrollFixture, asideStableDriver)
}

const asideFixture = `
const thoughts=['被夸得有点开心，先偷偷收好。','<raw> 这句也只是心里话。'];
const asideRows=[
 {id:1,transcript_seq:1,delivery_key:'say:a',kind:'bot',text:'嘿嘿，谢谢你。',time:new Date().toISOString()},
 ...thoughts.map((text,i)=>({id:i+2,transcript_seq:i+3,delivery_key:'aside:'+(i+3),kind:'aside',text,time:new Date().toISOString()}))
];
const asideFetch=window.fetch;
window.fetch=(path,opts)=>String(path).endsWith('/chat')?Promise.resolve(json({messages:asideRows})):asideFetch(path,opts);
`

const asideScrollFixture = `
asideRows.unshift(...Array.from({length:32},(_,i)=>({kind:'bot',delivery_key:'say:history'+i,text:('第 '+(i+1)+' 条历史回复，检查打开旁注时的阅读位置。').repeat(5),time:new Date().toISOString()})));
asideRows.forEach((row,i)=>{row.id=i+1;row.transcript_seq=i+1});
`

const asideStableDriver = `
const $=s=>document.querySelector(s),assert=(v,m)=>{if(!v)throw Error(m)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async f=>{for(let i=0;i<500;i++){if(f())return;await sleep(10)}throw Error('timeout')};
(async()=>{
 await waitFor(()=>$('.aside-toggle')&&!$('.thread-loading-cover'));
 await document.fonts.ready;await sleep(200);
 const thread=$('#thread'),head=$('.aside-toggle'),drawer=$('.aside-body');
 const bubble=[...thread.querySelectorAll('.msg.bot .bot-bubble')].at(-1);
 assert(thread.scrollHeight>thread.clientHeight+400,'fixture must scroll');
 const sample=async toggle=>{
  const before=bubble.getBoundingClientRect().top,top=thread.scrollTop;
  let delta=0;toggle();
  const end=performance.now()+450;
  while(performance.now()<end){await new Promise(requestAnimationFrame);delta=Math.max(delta,Math.abs(bubble.getBoundingClientRect().top-before))}
  assert(delta<1,'aside shifted message by '+delta.toFixed(2)+'px, scroll '+top+' -> '+thread.scrollTop);
  return delta;
 };
 const opened=await sample(()=>head.click());assert(!drawer.hidden,'open failed');
 const bar=head.closest('.msg-meta-bar');
 assert(bar&&head.previousElementSibling?.getAttribute('aria-label')==='复制','aside action must follow copy');
 assert(head.closest('.msg')===bubble.closest('.msg'),'aside attached to wrong reply');
 assert(head.getBoundingClientRect().height===head.previousElementSibling.getBoundingClientRect().height,'action row height changed');
 await sleep(2400);assert($('.aside-toggle')===head&&!drawer.hidden,'poll reset aside');
 const closed=await sample(()=>head.click());assert(drawer.hidden,'close failed');
 const height=thread.scrollHeight,before=bubble.getBoundingClientRect().top;
 head.dispatchEvent(new PointerEvent('pointerover',{bubbles:true,pointerType:'mouse'}));
 await waitFor(()=>{const tip=$('[role="tooltip"]');return tip&&!tip.hidden});
 assert(thread.scrollHeight===height&&Math.abs(bubble.getBoundingClientRect().top-before)<1,'hover moved chat');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.stickPerformance=JSON.stringify({asideOpenShift:opened,asideCloseShift:closed});
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const asideDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn)=>{for(let i=0;i<500;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error('timeout')};
(async()=>{
 await waitFor(()=>$('#thread .source-event-row')&&!$('.thread-loading-cover'));
 await document.fonts.ready;
 // Let the initial stick/ResizeObserver scroll finish before hovering.
 await new Promise(r=>setTimeout(r,600));
 const row=$('#thread .aside-event'),head=$('#thread .msg-meta-bar .aside-toggle'),drawer=row.querySelector('.event-raw');
 assert(row._source.kind==='aside'&&row._texts.length===2,'source seam/group');
 assert(head.textContent===''&&head.querySelectorAll('svg').length===1,'icon disclosure');
 assert(head.getAttribute('aria-label')==='查看心里话 · 2 条','accessible grouped action');
 assert(document.getElementById(head.getAttribute('aria-controls'))===drawer,'disclosure target');
 assert(drawer.hidden&&head.getAttribute('aria-expanded')==='false','starts closed');
 assert($('#thread').querySelectorAll('.msg.bot').length===1&&!row.closest('.msg'),'aside became bubble');
 head.dispatchEvent(new PointerEvent('pointerover',{bubbles:true,pointerType:'mouse'}));
 await waitFor(()=>{const tip=document.querySelector('[role="tooltip"]');return tip&&!tip.hidden&&tip.textContent==='查看心里话 · 2 条\n\n'+thoughts.join('\n\n')&&tip.getBoundingClientRect().width>0});
 const tip=document.querySelector('[role="tooltip"]');
 assert(!tip.hidden&&tip.getBoundingClientRect().width>0,'hover tip invisible');
 head.click();assert(!drawer.hidden&&drawer.textContent===thoughts.join('\n\n'),'touch/click drawer');
 assert(!drawer.querySelector('raw')&&head.getAttribute('aria-expanded')==='true','unsafe text/expanded');
 assert(head.getAttribute('aria-label')==='收起心里话 · 2 条'&&head.dataset.tip.startsWith('收起心里话 · 2 条\n\n'),'open action label');
 row.dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true}));
 assert(!document.querySelector('.msg-menu:not([hidden])'),'aside has reply menu');
 const selected=document.querySelector('[data-bot="2号小伙子"]');
 assert(!selected.textContent.includes(thoughts[1]),'aside leaked to preview');
 await new Promise(r=>setTimeout(r,2400));
 assert($('#thread .source-event-row')===row&&!drawer.hidden,'poll reset drawer/node');
 head.click();assert(drawer.hidden&&head.getAttribute('aria-label')==='查看心里话 · 2 条','close failed');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
