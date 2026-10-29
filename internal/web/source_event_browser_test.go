package web

import "testing"

func TestSourceEventBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+sourceEventFixture, sourceEventDriver)
}

const sourceEventFixture = `
const event=(source,sender,text)=>message('user','<untrusted_event source="'+source+'" sender="'+sender+'" type="message">\n'+text+'\n</untrusted_event>');
fixture.messages['2号小伙子']=[event('cli','Codex','first <raw>'),event('cli','Codex','second'),event('bot:Codex','Codex','bot secret'),event('cli','Other','other'),message('user','boundary'),event('cli','Codex','third'),event('shell','sh','hidden')];
const originalFetch=window.fetch; window.peerCalls=[];
window.fetch=(path,opts)=>{if(String(path).includes('/peers/')){peerCalls.push(String(path));return Promise.resolve(json({messages:[]}))} return originalFetch(path,opts)};
`
const sourceEventDriver = `
const $=s=>document.querySelector(s), assert=(v,m)=>{if(!v)throw Error(m)};
const waitFor=async(fn)=>{for(let i=0;i<500;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error('timeout')};
(async()=>{
 await waitFor(()=>document.querySelectorAll('#thread .source-event-row').length===4);
 const rows=[...document.querySelectorAll('#thread .source-event-row')], heads=rows.map(r=>r.querySelector('button'));
 assert(heads[0].textContent.includes('Codex')&&heads[0].dataset.count==='2','CLI grouping');
 assert(heads[1].dataset.peer==='Codex'&&heads[1].dataset.count==='1','bot isolated from same-name CLI');
 assert(heads[3].dataset.count==='1','user message ends group');
 const img=heads[0].querySelector('img'); assert(img&&decodeURIComponent(img.src).includes(window.XBotAvatar.face('Codex')),'portrait seed');
 const drawer=rows[0].querySelector('.event-raw'); assert(drawer.hidden,'starts closed'); heads[0].click();
 assert(!drawer.hidden&&drawer.textContent==='first <raw>\n\nsecond','raw text');
 assert(heads[0].getAttribute('aria-expanded')==='true'&&peerCalls.length===0,'CLI click called peer');
 heads[0].click();assert(drawer.hidden,'close failed'); heads[1].click(); await waitFor(()=>peerCalls.length>0);
 assert(peerCalls[0].includes('/peers/Codex'),'bot click did not open exchange');
 assert(!$('#thread').textContent.includes('hidden'),'shell leaked');
 assert(testErrors.length===0,testErrors.join('; ')); document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
