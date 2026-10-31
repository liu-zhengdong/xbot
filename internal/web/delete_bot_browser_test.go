package web

import "testing"

func TestDeleteBotConfirmBrowser(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			runChatStreamBrowser(t, chatStreamFixture+deleteBotFixture+`localStorage.setItem('xbot-theme','`+theme+`');`, deleteBotDriver)
		})
	}
}

const deleteBotFixture = `
let deleteFailure='',deleteCalls=[];
const deleteFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(opts.method==='DELETE'&&path.startsWith('/api/bots/')){
  deleteCalls.push(path);
  if(deleteFailure)return new Response(JSON.stringify({error:deleteFailure}),{status:409,headers:{'Content-Type':'application/json'}});
  bots.splice(bots.findIndex(b=>path.endsWith('/'+encodeURIComponent(b.name))),1);
  return json({name:'另一个Bot'});
 }
 return deleteFetch(path,opts);
};
`

const deleteBotDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<400;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const openDelete=async()=>{
 const row=$('[data-bot-menu][data-bot="另一个Bot"]');
 row.dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true}));
 await waitFor(()=>$('.panel-more'),'menu missing');
 [...document.querySelectorAll('.panel-more button')].find(b=>b.textContent==='删除 Bot').click();
 await waitFor(()=>$('.nestcard'),'nestcard confirm missing');
};
(async()=>{
 await waitFor(()=>$('[data-bot-menu][data-bot="另一个Bot"]'),'sidebar missing');
 await openDelete();
 const card=$('.nestcard'),cancel=card.querySelector('.btn.sec'),ok=card.querySelector('.btn.danger');
 assert(card.getAttribute('role')==='dialog'&&card.getAttribute('aria-modal')==='true','dialog semantics');
 assert(card.querySelector('h3').textContent==='删除 另一个Bot？','display name title');
 assert(card.querySelector('.lead').textContent==='删掉后，和它的对话也会一起没了，不能恢复。','lead');
 assert(!card.querySelector('.note, .avatar')&&!card.querySelector('.text-btn'),'old confirm skin');
 assert(cancel.textContent==='取消'&&ok.textContent==='删除','actions');
 const skin=getComputedStyle(ok),lead=getComputedStyle(card.querySelector('.lead'));
 assert(skin.backgroundColor!=='rgba(0, 0, 0, 0)'&&skin.color!==skin.backgroundColor,'solid danger button');
 assert(lead.backgroundColor==='rgba(0, 0, 0, 0)','lead has a gray box');
 assert(ok.getBoundingClientRect().left>cancel.getBoundingClientRect().right,'action order');
 assert(document.activeElement===cancel,'safe initial focus');
 cancel.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',bubbles:true,cancelable:true}));assert(document.activeElement===ok,'tab');
 ok.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',bubbles:true,cancelable:true}));assert(document.activeElement===cancel,'tab wraps');
 if(new URLSearchParams(location.search).get('shot')){document.body.dataset.testResult='passed';return}
 cancel.click();assert(!$('.nestcard')&&deleteCalls.length===0,'cancel deleted');
 await openDelete();document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
 assert(!$('.nestcard')&&deleteCalls.length===0,'Escape deleted');
 for(const [raw,human] of [['the primary cannot be deleted','主 Bot 不能删，先把主 Bot 交给另一个。'],['the last bot cannot be deleted','最后一个 Bot 不能删。']]){
  await openDelete();deleteFailure=raw;$('.nestcard .btn.danger').click();
  await waitFor(()=>!$('.nestcard .sk-error').hidden,'error missing');
  assert($('.nestcard .sk-error').textContent===human,'raw error leaked');
  assert(!$('.nestcard .btn.danger').disabled&&bots.length===2,'blocked delete lost bot');
  $('.nestcard .btn.sec').click();
 }
 deleteFailure='';await openDelete();$('.nestcard .btn.danger').click();
 await waitFor(()=>!$('.nestcard')&&!$('[data-bot-menu][data-bot="另一个Bot"]'),'delete did not refresh sidebar');
 assert(deleteCalls.length===3&&deleteCalls.at(-1)==='/api/bots/'+encodeURIComponent('另一个Bot'),'wrong delete request');
 assert(bots.length===1&&$('[data-bot-menu]').dataset.bot===bots[0].name,'home bot lost');
 assert(testErrors.length===0,testErrors.join(';'));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
