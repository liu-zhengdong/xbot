package web

import "testing"

func TestVoiceClusterBrowser(t *testing.T) {
	runTodoBrowser(t, voiceClusterFixture, voiceClusterDriver)
}

const voiceClusterFixture = bubbleSplitFixture + `
const clusterText='笑这么大声，是不是听着像播新闻的？\n\n我猜两个都挺正经，跟表情包不是一个物种。\n\n要不我再拿 Tingting 说句短点的试试，短句可能没那么端着。';
const clusterUser={...message('user',clusterText+'\n\n短句版。'),audios:[splitAudios[0]]};
const clusterSay=sayRows(clusterText+'\n\n短句版。');
fixture.messages[bots[0].name]=[clusterUser,...clusterSay,{...message('assistant','SILENT'),audios:[splitAudios[0]]}];
HTMLMediaElement.prototype.load=function(){Object.defineProperty(this,'duration',{value:1,configurable:true});this.dispatchEvent(new Event('loadedmetadata'))};
`

const voiceClusterDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const check=(row,label)=>{
 const parts=[...row.firstElementChild.children].filter(n=>n.matches('.bubble,.bot-bubble')),text=parts[0],voice=parts[1];
 assert(parts.length===2,label+': expected text + audio/caption, got '+parts.length);
 assert(text.querySelectorAll('p').length===3&&text.textContent.includes('Tingting'),label+': paragraphs not in one bubble');
 assert(voice.querySelectorAll('audio').length===1&&voice.querySelector('.v-caption')?.textContent==='短句版。',label+': caption not attached');
 const t=text.getBoundingClientRect(),v=voice.getBoundingClientRect();
 assert(v.width>=140&&v.width<=170&&v.width<t.width-40,label+': short voice stretched '+v.width+'/'+t.width);
 assert(Math.abs((row.matches('.user')?t.right-v.right:t.left-v.left))<1,label+': cluster alignment');
 assert(Math.abs(v.top-t.bottom-6)<1,label+': cluster gap');
 const play=voice.querySelector('.v-play').getBoundingClientRect(),wave=voice.querySelector('.v-wave').getBoundingClientRect(),time=voice.querySelector('.v-time').getBoundingClientRect();
 assert(Math.abs(play.height-32)<1&&Math.abs(wave.height-play.height)<1,label+': play/wave heights');
 assert(Math.abs(play.top+play.height/2-wave.top-wave.height/2)<1&&Math.abs(play.top+play.height/2-time.top-time.height/2)<1,label+': row centers');
 assert(voice.querySelectorAll('.v-wave i').length===10&&!voice.querySelector('.v-bar'),label+': waveform missing');
};
(async()=>{
 const original=JSON.stringify(fixture.messages);
 await waitFor(()=>$('#thread .msg.bot audio'),'cluster missing');
 if(new URLSearchParams(location.search).has('shot')){document.body.dataset.testResult='passed';return}
 check($('#thread .msg.user'),'user');check($('#thread .msg.bot'),'bot');
 assert(JSON.stringify(fixture.messages)===original,'render mutated transcript');
 // The real delivered-message grouping also supplies a separate final short say.
 const separate=sayRows(clusterText),tail=sayRows('短句版。');
 fixture.messages[bots[0].name]=[clusterUser,...separate,...tail,{...message('assistant','SILENT'),audios:[splitAudios[0]]}];
 document.dispatchEvent(new Event('visibilitychange'));
 await waitFor(()=>document.querySelectorAll('#thread .msg.bot').length===2&&$('#thread .msg.bot .v-caption'),'separate say caption');
 assert(document.querySelectorAll('#thread .msg.bot .bot-bubble').length===2,'same turn became three bubbles');
 // Ambiguous short prose stays in the text, rather than becoming a caption.
 clusterUser.content=clusterText+'\n\n明天我再发音频。';
 document.dispatchEvent(new Event('visibilitychange'));
 await waitFor(()=>$('#thread .msg.user').textContent.includes('明天我再发音频。'),'ambiguous text missing');
 assert(!$('#thread .msg.user .v-caption')&&$('#thread .msg.user .bubble').querySelectorAll('p').length===4,'ambiguous prose detached');
 const audio=$('#thread .msg.user audio'),voice=audio.closest('.bubble'),short=voice.getBoundingClientRect().width;
 Object.defineProperty(audio,'duration',{value:60,configurable:true});audio.dispatchEvent(new Event('loadedmetadata'));
 assert(voice.getBoundingClientRect().width>short,'wave width did not follow duration');
 // Unknown metadata falls back to the compact waveform.
 Object.defineProperty(audio,'duration',{value:Infinity,configurable:true});audio.dispatchEvent(new Event('loadedmetadata'));
 assert(Math.abs(voice.querySelector('.v-wave').getBoundingClientRect().width-56)<1,'unknown duration widened waveform');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`
