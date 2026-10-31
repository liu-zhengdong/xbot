package web

import "testing"

func TestProfileRefreshWhileStreamingBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+profileRefreshFixture, profileRefreshDriver)
}

const profileRefreshFixture = `
const profileFetch=window.fetch;
fixture.botReads=0;
window.fetch=async(path,opts={})=>{
 if(path==='/api/bots'){
  fixture.botReads++;
  if(fixture.holdProfile)await new Promise(r=>fixture.releaseProfile=r);
  if(fixture.failProfile){fixture.failProfile=false;throw Error('temporary list failure')}
 }
 return profileFetch(path,opts);
};
`

const profileRefreshDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<200;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const face=(name,head=false)=>(head?$('#title .avatar'):document.querySelector('[data-bot="'+name+'"] .avatar'))?.outerHTML;
const send=async value=>{
 const n=fixture.streams.length;const box=$('#input');box.textContent=value;box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length>n&&$('#thread .live'),'send did not start');
 return fixture.streams.at(-1);
};
(async()=>{
 const name='2号小伙子';
 await waitFor(()=>$('#title').textContent.includes(name)&&face(name)&&!$('.thread-loading-cover'),'boot failed');
 const stream=await send('给自己换张头像');
 const oldFace=face(name),oldHead=face(name,true),reads=fixture.botReads;
 bots[0].avatar_seed='Ada#2';
 emit(stream,'tool',{name:'bot_update_profile',summary:'avatar_seed Ada#2'});
 await new Promise(r=>setTimeout(r,30));
 assert(fixture.botReads===reads&&face(name)===oldFace,'refreshed before profile save');
 if(new URLSearchParams(location.search).get('shot')==='before'){document.body.dataset.testResult='passed';return}
 const started=performance.now();
 emit(stream,'result',{name:'bot_update_profile',output:'profile saved'});
 await waitFor(()=>face(name)!==oldFace&&face(name,true)!==oldHead,'saved profile did not repaint rail and header during stream');
 document.body.dataset.profileRefreshMs=String(Math.round(performance.now()-started));
 assert($('#thread .live'),'profile refresh ended the stream');
 if(new URLSearchParams(location.search).get('shot')==='after'){document.body.dataset.testResult='passed';return}
 const unchanged=fixture.botReads;
 emit(stream,'result',{name:'read_file',output:'ok'});
 await new Promise(r=>setTimeout(r,30));
 assert(fixture.botReads===unchanged,'unrelated result refreshed profile');
 // A slow list fetch must not pause incoming text or steal the next Bot's header.
 const firstSavedFace=face(name);
 fixture.holdProfile=true;bots[0].avatar_seed='Ada#3';
 emit(stream,'result',{name:'bot_update_profile',output:'profile saved'});
 await waitFor(()=>fixture.releaseProfile,'profile read not started');
 say(stream,'继续处理。');
 await waitFor(()=>$('#thread').textContent.includes('继续处理。'),'profile fetch blocked the stream');
 $('[data-bot="另一个Bot"]').click();
 await waitFor(()=>$('#title').textContent.includes('另一个Bot'),'switch failed');
 const otherHead=face('另一个Bot',true);
 fixture.holdProfile=false;fixture.releaseProfile();
 await waitFor(()=>face(name)!==firstSavedFace,'delayed list missing');
 await new Promise(r=>setTimeout(r,50));
 assert($('#title').textContent.includes('另一个Bot')&&face('另一个Bot',true)===otherHead,'profile read stole selected header');
 finish(stream,'换好了。');
 // A refresh error must not reject readSSE or lose the actual reply.
 const next=await send('继续');fixture.failProfile=true;
 emit(next,'result',{name:'bot_update_profile',output:'profile saved'});
 say(next,'仍能回复。');
 await waitFor(()=>$('#thread').textContent.includes('仍能回复。'),'list failure broke stream');
 finish(next,'仍能回复。');
 await waitFor(()=>!$('#thread .live'),'stream did not finish');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
