package web

import "testing"

func TestImggenDiscoverBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture+imggenDiscoverFixture, imggenDiscoverDriver)
}

const imggenDiscoverFixture = `
const prompt='very small simple black and white cat, '.repeat(15)+'猫😀';
const generatedPart={type:'hosted',text:'[厂商生图: 已保存到工作区 generated/real.png]',hosted:{id:'ig_1',name:'image_generation',status:'completed',query:prompt}};
const imageID='0123456789abcdef0123456789abcdef.png';
fixture.messages['2号小伙子']=[{...message('assistant','已用挂图方法挂上，文件在 generated/real.png。'),parts:[generatedPart],images:[imageID]}];
// HTTP is scripted in this file fixture, including the existing paste route.
const imageSrc=Object.getOwnPropertyDescriptor(HTMLImageElement.prototype,'src');
Object.defineProperty(HTMLImageElement.prototype,'src',{...imageSrc,set(value){
 if(String(value).includes('/paste/')){this.dataset.deliveryUrl=value;value='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6qS8AAAAASUVORK5CYII='}
 imageSrc.set.call(this,value);
}});
fixture.generatedCalls=[];fixture.canOpen=false;fixture.generatedFail=false;fixture.openFail=false;
fixture.generatedPath='/server/xbot/work/2号小伙子/generated';
Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{fixture.copied=text}},configurable:true});
document.execCommand=cmd=>{if(cmd==='copy'){fixture.copied=document.activeElement.value;return true}return false};
const originalFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(path==='/api/session')return json({authed:true,owner:'owner',owner_name:'政东',version:'dev',tip:'abc1234'});
 if(String(path).endsWith('/generated')){
  fixture.generatedCalls.push(path);
  if(fixture.generatedFail)return new Response(JSON.stringify({error:'读取失败'}),{status:500});
  return json({path:fixture.generatedPath,can_open:fixture.canOpen});
 }
 if(String(path).endsWith('/open-workdir?folder=generated')){
  fixture.opened=path;
  if(fixture.openFail)return new Response(JSON.stringify({error:'打开失败'}),{status:500});
  return json({status:'ok'});
 }
 return originalFetch(path,opts);
};
`

const imggenDiscoverDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const visible=n=>!!n&&n.getClientRects().length>0;
const button=(host,text)=>[...host.querySelectorAll('button')].find(b=>b.textContent===text);
const openGenerated=async()=>{
 $('[data-bot="2号小伙子"]').dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true}));
 button($('.panel-more'),'查看生成的图片').click();await waitFor(()=>visible($('#modal')),'generated menu missing');
};
(async()=>{
 await waitFor(()=>$('#thread .gallery img')?.naturalWidth>0&&!$('#shell').hidden,'history image did not load');
 assert(!$('#thread .hs-step'),'image generation has a dedicated hosted block');
 assert(!$('#thread').textContent.includes('查看 generated/'),'directory used as delivery');
 assert($('#thread .gallery img').dataset.deliveryUrl.endsWith('/paste/'+imageID),'gallery did not use existing paste route');
 const shot=new URLSearchParams(location.search).get('shot');
 if(shot==='reply'){document.body.dataset.testResult='passed';return}
 $('#head-chip').click();await waitFor(()=>$('#panel [role=tab]'),'panel missing');
 [...document.querySelectorAll('#panel [role=tab]')].find(n=>n.textContent==='轨迹').click();
 await waitFor(()=>$('.trace-row'),'generation absent from ordinary trace');
 $('.trace-row').click();
 assert($('.trace-call-name').textContent==='image_generation','wrong trace call');
 assert($('.trace-args').textContent.includes(prompt),'trace lost prompt');
 assert($('.trace-out').textContent.includes('generated/real.png')&&!$('.trace-out').textContent.includes('已挂'),'trace lost receipt');
 assert(!$('#panel img[src*="paste"]'),'trace displayed an image');
 if(shot==='trace'){document.body.dataset.testResult='passed';return}
 // Preserve the optional workspace menu; it is no longer a reply step.
 await openGenerated();
 assert($('#modal input').readOnly&&$('#modal input').value===fixture.generatedPath,'server path is not visible');
 assert(!button($('#modal'),'打开目录'),'browser has fake desktop action');
 $('#modal-ok').click();await waitFor(()=>$('#modal').hidden,'copy did not close');
 assert(fixture.copied===fixture.generatedPath,'wrong copied path');
 fixture.canOpen=true;await openGenerated();
 fixture.openFail=true;button($('#modal'),'打开目录').click();await waitFor(()=>visible($('#modal-error')),'desktop failure silent');
 fixture.openFail=false;button($('#modal'),'打开目录').click();await waitFor(()=>!button($('#modal'),'打开目录').disabled,'desktop call pending');
 assert(fixture.opened.endsWith('/open-workdir?folder=generated'),'desktop did not reuse endpoint');$('#modal-cancel').click();
 fixture.generatedFail=true;
 $('[data-bot="2号小伙子"]').dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true}));
 button($('.panel-more'),'查看生成的图片').click();await new Promise(r=>setTimeout(r,30));
 assert($('#modal').hidden,'failed lookup opened a fake directory');fixture.generatedFail=false;
 await XBotSettings.open('about');
 assert([...document.querySelectorAll('[data-settings-version]')].some(n=>visible(n)&&n.textContent==='dev · abc1234'),'about loses build tip');
 $('#settings-copy-version').click();await waitFor(()=>fixture.copied==='dev · abc1234','copy loses tip');XBotSettings.close();
 // The running hosted event uses the same live status as a local tool. No
 // picture appears until the final reply has an actual Images reference.
 const box=$('#input');box.textContent='画猫并告诉我图在哪';box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length&&$('#thread .live'),'stream missing');
 const stream=fixture.streams.at(-1);
 emit(stream,'part',{hosted:{id:'ig_live',name:'image_generation',status:'in_progress'}});
 await waitFor(()=>$('#thread .live .live-status')?.textContent.includes('生成图片'),'ordinary working status missing');
 assert(!$('#thread .live .hs-step')&&!$('#thread .live .gallery'),'running generation rendered a special block or picture');
 emit(stream,'part',{...generatedPart,hosted:{...generatedPart.hosted,id:'ig_live'}});
 await new Promise(r=>setTimeout(r,20));
 assert(!$('#thread .live .gallery'),'completion part alone displayed a picture');
 // Simulate the subsequent send_message_to_user success and final Entry.Images.
 fixture.messages[stream.bot].push({...message('assistant','已用挂图方法挂上。'),parts:[{...generatedPart,hosted:{...generatedPart.hosted,id:'ig_live'}}],images:[imageID]});
 emit(stream,'done',{text:'已用挂图方法挂上。'});stream.controller.close();
 await waitFor(()=>!$('#thread .live')&&$('#thread').querySelectorAll('.gallery img').length===2,'final reply did not attach image');
 assert(!$('#thread .hs-step'),'history restored dedicated image component');
 // A save-only final reply has no gallery, even with a successful hosted part.
 fixture.messages[stream.bot].push(message('user','只存不挂'),{...message('assistant',''),parts:[{...generatedPart,hosted:{...generatedPart.hosted,id:'ig_saved'}}]},...sayRows('仅保存于 generated/saved.png。'));
 // Failed delivery remains plain text without Images, never a gallery.
 fixture.messages[stream.bot].push(message('user','挂大图'),{...message('assistant',''),parts:[{type:'hosted',text:'[厂商生图: 已保存到工作区 generated/large.png]',hosted:{id:'ig_failed',name:'image_generation',status:'completed',query:'大图'}}]},...sayRows('挂图失败，文件仍在 generated/large.png。'));
 $('[data-bot="另一个Bot"]').click();await waitFor(()=>$('#title').textContent.includes('另一个Bot'),'switch failed');
 $('[data-bot="2号小伙子"]').click();await waitFor(()=>$('#thread').textContent.includes('generated/large.png'),'history not restored');
 assert($('#thread').querySelectorAll('.gallery').length===2,'failed delivery created gallery');
 assert(!$('#thread .hs-step'),'failed delivery created special component');
 assert(testErrors.length===0,testErrors.join(';'));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
