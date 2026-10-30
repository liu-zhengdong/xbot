// A wall-clock CDP driver: virtual-time --dump-dom advances timers before the
// native microphone and audio decoder get CPU time.
const {spawn}=require('node:child_process');
const fs=require('node:fs/promises');
const path=require('node:path');
const [chrome,url,profile]=process.argv.slice(2);
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const child=spawn(chrome,['--headless=new','--disable-gpu','--no-sandbox','--no-first-run','--no-default-browser-check','--use-fake-device-for-media-stream','--use-fake-ui-for-media-stream','--autoplay-policy=no-user-gesture-required','--remote-debugging-port=0','--user-data-dir='+profile,url],{stdio:'ignore'});
let socket;
(async()=>{
 let port;
 for(let i=0;i<200;i++){try{port=(await fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0];break}catch{}await sleep(50)}
 if(!port)throw Error('Chrome did not start');
 const tabs=await (await fetch('http://127.0.0.1:'+port+'/json')).json();
 const tab=tabs.find(t=>t.type==='page');if(!tab)throw Error('missing Chrome page');
 socket=new WebSocket(tab.webSocketDebuggerUrl);await new Promise((r,j)=>{socket.onopen=r;socket.onerror=j});
 let id=0;const pending=new Map();
 socket.onmessage=event=>{const m=JSON.parse(event.data);if(m.id){const call=pending.get(m.id);pending.delete(m.id);if(m.error)call.reject(Error(m.error.message));else call.resolve(m.result)}};
 const send=(method,params={})=>new Promise((resolve,reject)=>{pending.set(++id,{resolve,reject});socket.send(JSON.stringify({id,method,params}))});
 const evaluate=async expression=>(await send('Runtime.evaluate',{expression,returnByValue:true})).result.value;
 for(let i=0;i<350;i++){
  const result=await evaluate('document.body?.dataset.testResult');
  if(result){if(result!=='passed')throw Error(result);console.log('passed '+await evaluate('document.body.dataset.voicePerformance||""'));return}
  await sleep(100);
 }
 throw Error('voice journey timed out: '+await evaluate('document.body?.innerText.slice(-1600)'));
})().catch(e=>{console.error(e.stack);process.exitCode=1}).finally(()=>{socket?.close();child.kill('SIGTERM')});
