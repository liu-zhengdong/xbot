package web

import "testing"

func TestAsideSourceBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+asideFixture, asideDriver)
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

const asideDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn)=>{for(let i=0;i<500;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error('timeout')};
(async()=>{
 await waitFor(()=>$('#thread .source-event-row')&&!$('.thread-loading-cover'));
 await document.fonts.ready;
 // Let the initial stick/ResizeObserver scroll finish before hovering.
 await new Promise(r=>setTimeout(r,600));
 const row=$('#thread .source-event-row'),head=row.querySelector('button'),drawer=row.querySelector('.event-raw');
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
