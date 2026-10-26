package web

import "testing"

// Real shipped page and settings entrypoint, with fake HTTP data only.
func TestSkillsTitleBrowser(t *testing.T) {
	runChatStreamBrowser(t, providersFixture+skillsTitleFixture, skillsTitleDriver)
}

const skillsTitleFixture = `
const skillRows=[
 {slug:'note-capture',name:'note-capture',title:'记一下',description:'我说记一下时，把内容整理到笔记里。这段长说明只出现在副行。'},
 {slug:'plain-skill',name:'plain-skill',description:'没有短名的技能也不能把长描述当标题。'},
 {slug:'legacy',name:'旧短名',description:'兼容已有技能。'}
];
const skillFetch=window.fetch;
let skillBots=[{name:'ada',enabled:true},{name:'bob',enabled:false}];
window.fetch=async(path,opts={})=>{
 if(path==='/api/skills')return json({skills:skillRows});
 if(path.startsWith('/api/skills/'))return json({...skillRows.find(r=>path.endsWith('/'+r.slug)),bots:skillBots});
 if(path.startsWith('/api/bots/')&&path.includes('/skills/')){
  const parts=path.split('/');skillBots.find(b=>b.name===parts[3]).enabled=parts.at(-1)==='enable';return json({});
 }
 return skillFetch(path,opts);
};
`
const skillsTitleDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<400;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
(async()=>{
 await waitFor(()=>window.XBotSettings,'settings not ready');
 await XBotSettings.open('skills');
 await waitFor(()=>$('.skrow'),'skill list missing');
 const rows=[...document.querySelectorAll('.skrow')];
 const titles=rows.map(r=>r.querySelector('b').textContent);
 assert(JSON.stringify(titles)===JSON.stringify(['记一下','plain-skill','旧短名']),'title priority: '+titles);
 assert(rows[0].querySelector('.hint').textContent===skillRows[0].description,'description lost');
 const shot=new URLSearchParams(location.search).get('shot');
 if(shot==='list'){document.body.dataset.testResult='passed';return}
 rows[0].click();await waitFor(()=>$('.skdetail-name'),'detail missing');
 assert($('.skdetail-name').textContent==='记一下','detail title');
 const switches=[...document.querySelectorAll('#settings-skills [role=switch]')];
 assert(switches.length===2,'two switches');
 assert(switches[0].getAttribute('aria-checked')==='true'&&switches[1].getAttribute('aria-checked')==='false','self defaults');
 if(shot==='detail'){document.body.dataset.testResult='passed';return}
 switches[1].click();await waitFor(()=>switches[1].getAttribute('aria-checked')==='true','manual enable');
 switches[0].click();await waitFor(()=>switches[0].getAttribute('aria-checked')==='false','manual disable');
 $('.setback').click();await waitFor(()=>$('.skrow'),'return list');
 document.querySelectorAll('.skrow')[1].click();await waitFor(()=>$('.skdetail-name'),'plain detail');
 assert($('.skdetail-name').textContent==='plain-skill','description became detail title');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
