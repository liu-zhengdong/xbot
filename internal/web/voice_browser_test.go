package web

import "testing"

func TestVoiceChatBrowser(t *testing.T) {
	runTodoBrowser(t, voiceBrowserFixture, voiceBrowserDriver)
}

const voiceBrowserFixture = `
localStorage.removeItem('xbot-panel-open');
const voiceID='0123456789abcdef0123456789abcdef.webm';
fixture.messages[bots[0].name].push({...message('user','文字草稿之外的语音。'),audios:[voiceID]});
fixture.messages[bots[0].name].push(...sayRows('收到，七点后会议室。'),{...message('assistant','SILENT'),audios:[voiceID]});
const voiceRequests=[],voiceStreams=[];
let denyMic=false,releaseMic=null;
Object.defineProperty(navigator,'mediaDevices',{value:{getUserMedia:async()=>{
 if(denyMic)throw Error('denied');
 if(releaseMic)await new Promise(r=>releaseMic=r);
 const stream={stopped:false,getTracks(){return [{stop:()=>{stream.stopped=true}}]}};voiceStreams.push(stream);return stream;
}}});
window.MediaRecorder=class {
 static isTypeSupported(type){return type.startsWith('audio/webm')}
 constructor(stream,options){this.stream=stream;this.mimeType=options.mimeType;this.state='inactive'}
 start(){this.state='recording'}
 stop(){this.state='inactive';Promise.resolve().then(()=>{this.ondataavailable({data:new Blob(['\\x1aE\\xdf\\xa3voice-fixture'],{type:this.mimeType})});this.onstop()})}
};
Object.defineProperty(HTMLMediaElement.prototype,'preload',{get(){return 'none'},set(){this.setAttribute('preload','none')}});
const mediaPaused=new WeakMap();
let playCount=0;
Object.defineProperty(HTMLMediaElement.prototype,'paused',{get(){return mediaPaused.get(this)!==false}});
HTMLMediaElement.prototype.play=function(){playCount++;mediaPaused.set(this,false);this.dispatchEvent(new Event('play'));return Promise.resolve()};
HTMLMediaElement.prototype.pause=function(){mediaPaused.set(this,true);this.dispatchEvent(new Event('pause'))};
HTMLMediaElement.prototype.load=function(){Object.defineProperty(this,'duration',{value:12,configurable:true});this.dispatchEvent(new Event('loadedmetadata'))};
const voiceFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).endsWith('/voice')){voiceRequests.push({kind:'upload',body:opts.body,path});return json({audio:voiceID})}
 if(String(path).endsWith('/messages')){
  const body=JSON.parse(opts.body);voiceRequests.push({kind:'message',body,path});
  fixture.messages[bots[0].name].push({...message('user',body.text),audios:body.audios});
  return json({status:'queued'});
 }
 return voiceFetch(path,opts);
};
`

const voiceBrowserDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const refresh=()=>document.dispatchEvent(new Event('visibilitychange'));
(async()=>{
 await waitFor(()=>$('#thread .msg.user audio')&&$('#thread .msg.bot audio'),'voice bubbles missing');
 const shot=new URLSearchParams(location.search).get('shot');
 if(shot==='recording'){$('#mic').click();await waitFor(()=>voiceStreams.length,'recording');document.body.dataset.testResult='passed';return}
 const mic=$('#mic').getBoundingClientRect(),send=$('#send').getBoundingClientRect();assert(mic.right<=send.left,'mic must be left of send');
 const user=$('#thread .msg.user'),bot=$('#thread .msg.bot'),audio=user.querySelector('audio'),botAudio=bot.querySelector('audio');
 assert(user.querySelector('.bubble').firstElementChild.contains(audio),'user audio is not first in same bubble');
 assert(bot.querySelector('.bot-bubble').firstElementChild.contains(botAudio),'bot audio is not above text');
 assert(playCount===0&&!audio.autoplay&&!botAudio.autoplay,'audio autoplayed');
 if(shot==='default'){document.body.dataset.testResult='passed';return}
 user.querySelector('.v-play').click();await waitFor(()=>!audio.paused,'user did not play');
 bot.querySelector('.v-play').click();assert(audio.paused&&!botAudio.paused,'two audios playing');
 bot.querySelector('.v-play').click();assert(botAudio.paused,'pause failed');
 Object.defineProperty(audio,'duration',{value:Infinity,configurable:true});audio.dispatchEvent(new Event('loadedmetadata'));
 assert(user.querySelector('.v-time').textContent==='','unknown duration shown as zero');
 Object.defineProperty(audio,'duration',{value:12,configurable:true});audio.dispatchEvent(new Event('loadedmetadata'));
 audio.dispatchEvent(new Event('error'));assert(!user.querySelector('.v-fail').hidden&&!user.querySelector('.v-retry').hidden,'retry absent');
 if(shot==='failure'){document.body.dataset.testResult='passed';return}
 user.querySelector('.v-retry').click();assert(!user.querySelector('.v-play').hidden&&!audio.paused,'retry failed');
 const reads=fixture.reads;refresh();await waitFor(()=>fixture.reads>reads,'poll');await sleep(80);
 assert($('#thread .msg.user')===user&&user.querySelector('audio')===audio&&!audio.paused,'unchanged poll replaced player');
 fixture.messages[bots[0].name][0].audios.push('1123456789abcdef0123456789abcdef.webm');refresh();
 await waitFor(()=>$('#thread .msg.user')!==user,'audios append was ignored');assert(audio.paused,'removed audio kept playing');
 assert($('#thread .msg.bot')===bot,'audios append rebuilt unrelated bot');
 const input=$('#input');input.textContent='保留我的文字草稿';input.dispatchEvent(new Event('input',{bubbles:true}));
 $('#mic').click();await waitFor(()=>voiceStreams.length===1,'mic did not start');
 assert(input.textContent==='保留我的文字草稿','recording altered draft');
 $('#rec-send').click();await waitFor(()=>voiceRequests.filter(r=>r.kind==='message').length===1,'audio not sent');
 const payload=voiceRequests.find(r=>r.kind==='message').body;
 assert(payload.text===''&&payload.audios[0]===voiceID&&!payload.images,'voice became text');
 assert(voiceRequests[0].body instanceof Blob,'upload not audio blob');
 assert(input.textContent==='保留我的文字草稿'&&voiceStreams[0].stopped,'draft lost or mic not released');
 await waitFor(()=>!$('#rec-bar').hidden===false,'recording did not close');
 $('#mic').click();await waitFor(()=>voiceStreams.length===2,'second recording');
 document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));await sleep(20);
 assert(voiceRequests.length===2&&voiceStreams[1].stopped,'cancel sent audio');
 denyMic=true;$('#mic').click();await waitFor(()=>$('#toast').textContent.includes('无法使用麦克风'),'permission failure missing');
 assert(!$('#composer').classList.contains('recording')&&input.textContent==='保留我的文字草稿','permission failure altered draft');
 denyMic=false;
 $('#mic').dispatchEvent(new PointerEvent('pointerdown',{button:0,pointerId:41,bubbles:true}));await sleep(280);await waitFor(()=>voiceStreams.length===3,'hold did not start');
 const box=$('#composer').getBoundingClientRect();document.dispatchEvent(new PointerEvent('pointerup',{pointerId:41,clientX:box.right-5,clientY:box.bottom-5,bubbles:true}));
 await waitFor(()=>voiceRequests.filter(r=>r.kind==='message').length===2,'hold release did not send');
 $('#mic').dispatchEvent(new PointerEvent('pointerdown',{button:0,pointerId:42,bubbles:true}));await sleep(280);await waitFor(()=>voiceStreams.length===4,'cancel hold');
 document.dispatchEvent(new PointerEvent('pointerup',{pointerId:42,clientX:0,clientY:0,bubbles:true}));await sleep(20);
 assert(voiceRequests.filter(r=>r.kind==='message').length===2&&voiceStreams[3].stopped,'slide-out sent audio');
 $('#mic').click();await waitFor(()=>voiceStreams.length===5,'switch recording');
 document.querySelector('[data-bot="'+bots[1].name+'"]').click();await sleep(80);
 assert(voiceStreams[4].stopped&&!$('#composer').classList.contains('recording'),'switch kept mic running');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`
