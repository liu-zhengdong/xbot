package web

import "testing"

func TestPersonaToolbarBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+personaToolbarFixture, personaToolbarDriver)
}

const personaToolbarFixture = `
let personaRows=[{id:'a',name:'甲',preview:'第一张'},{id:'b',name:'乙',preview:'第二张'}];
const personaFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 path=String(path);
 if(path==='/api/persona-cards')return json({cards:personaRows});
 if(path.startsWith('/api/persona-cards?q='))return json({cards:personaRows.filter(r=>r.name.includes(decodeURIComponent(path.split('=')[1])))});
 return personaFetch(path,opts);
};
`
const personaToolbarDriver = `
const $=s=>document.querySelector(s), assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<400;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
(async()=>{
 await waitFor(()=>window.XBotSettings,'settings not ready');
 await XBotSettings.open('persona');
 await waitFor(()=>$('#settings-persona .skrow'),'cards missing');
 const pane=$('#settings-persona'), all=pane.querySelector('.pc-segment'), search=pane.querySelector('input[type=search]'), add=pane.querySelector('.pc-add'), imp=[...pane.querySelectorAll('button')].find(b=>b.textContent==='导入');
 const rects=[all,search,add,imp].map(n=>n.getBoundingClientRect());
 assert(rects.every(r=>Math.abs(r.top-rects[0].top)<4),'toolbar controls must share a desktop row');
 assert(rects.every((r,i)=>!i||rects[i-1].right<=r.left),'toolbar order');
 if(new URLSearchParams(location.search).has('shot')){document.body.dataset.testResult='passed';return}
 all.click();assert(all.getAttribute('aria-pressed')==='true'&&pane.querySelectorAll('.skrow').length===2,'all filter');
 search.value='甲';search.dispatchEvent(new Event('input',{bubbles:true}));
 await waitFor(()=>pane.querySelectorAll('.skrow').length===1,'search did not filter');
 assert(pane.querySelector('.skrow').textContent.includes('甲'),'wrong search result');
 search.value='';search.dispatchEvent(new Event('input',{bubbles:true}));
 assert(pane.querySelectorAll('.skrow').length===2,'search clear');
 add.click();await waitFor(()=>pane.querySelector('.pc-name'),'new card did not open');
 pane.querySelector('.setback').click();await waitFor(()=>pane.querySelector('.pc-add'),'list did not return');
 [...pane.querySelectorAll('button')].find(b=>b.textContent==='导入').click();await waitFor(()=>document.querySelector('.nestlayer'),'import did not open');
 XBotSettings.close();personaRows=[];await XBotSettings.open('persona');
 await waitFor(()=>pane.querySelector('.skempty'),'empty library missing');
 assert(!pane.querySelector('.sktoolbar')&&!pane.querySelector('.pc-segments'),'empty library has toolbar');
 assert([...pane.querySelectorAll('button')].some(b=>b.textContent==='写一张')&&[...pane.querySelectorAll('button')].some(b=>b.textContent==='导入'),'empty actions missing');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
