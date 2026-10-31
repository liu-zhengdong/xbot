package web

import "testing"

func TestMessageBubbleSplitBrowser(t *testing.T) {
	runTodoBrowser(t, bubbleSplitFixture, bubbleSplitDriver)
}

func TestMessageBubbleSplitLegacyBrowser(t *testing.T) {
	runTodoBrowser(t, bubbleSplitFixture+`
fixture.messages[bots[0].name]=[quoted,{...message('assistant',splitText),reply_to:quoted.seq,time:'2026-10-26T00:00:00Z'}];
`, `
(async()=>{
 for(let i=0;i<1000;i++){
  const bot=document.querySelector('#thread .msg.bot');
  if(bot){
   const bubbles=bot.querySelectorAll('.bot-bubble');
   if(bubbles.length!==1||bubbles[0].querySelectorAll('p').length!==3||!bubbles[0].textContent.includes('第一段第二行。'))throw Error('legacy paragraphs not combined');
   if(bot.querySelectorAll('.quote-block').length!==1||!bot.firstElementChild.firstElementChild.matches('.quote-block'))throw Error('legacy quote misplaced');
   document.body.dataset.testResult='passed';return;
  }
  await new Promise(r=>setTimeout(r,10));
 }
 throw Error('legacy message missing');
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}

const bubbleSplitFixture = `
localStorage.removeItem('xbot-panel-open');
const splitText='第一段第一行。\n第一段第二行。\n\n第二段。\n \t\n第三段。';
const splitImages=['1'.repeat(32)+'.png','2'.repeat(32)+'.png'];
const splitAudios=['1'.repeat(32)+'.webm','2'.repeat(32)+'.webm'];
const quoted=message('user','被引用的原话。');
const splitUser={...message('user',splitText),images:splitImages,audios:splitAudios,reply_to:quoted.seq};
const splitSays=sayRows(splitText);
const splitBot={...message('assistant','SILENT'),images:splitImages,audios:splitAudios,reply_to:splitUser.seq};
fixture.messages[bots[0].name].push(quoted,splitUser,...splitSays,splitBot);
// Script media delivery only; the shipped projection, send and poll paths run.
const splitImageSrc=Object.getOwnPropertyDescriptor(HTMLImageElement.prototype,'src');
Object.defineProperty(HTMLImageElement.prototype,'src',{...splitImageSrc,set(value){
 if(String(value).includes('/paste/'))value='data:image/svg+xml,'+encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="160" height="90"><rect width="160" height="90" fill="#aaa"/></svg>');
 splitImageSrc.set.call(this,value);
}});
HTMLMediaElement.prototype.load=function(){};
Object.defineProperty(HTMLMediaElement.prototype,'src',{set(value){this.dataset.deliveryUrl=value}});
let splitPayload;
const splitFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 if(String(path).endsWith('/messages')){
  splitPayload=JSON.parse(opts.body);
  return new Promise(()=>{}); // Hold the optimistic row for inspection.
 }
 return splitFetch(path,opts);
};
`

const bubbleSplitDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1000;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const bubbles=row=>[...row.querySelectorAll(':scope > .bubble-col > .bubble, :scope > .body > .bot-bubble')];
const check=(row,label)=>{
 const parts=bubbles(row);
 assert(parts.length===3,label+': expected one text and two audio bubbles, got '+parts.length);
 for(let i=1;i<3;i++)assert(parts[i].querySelectorAll('audio').length===1&&!parts[i].querySelector('.prose,.gallery'),label+': audio not isolated');
 assert(parts[0].textContent.includes('第一段第一行。')&&parts[0].textContent.includes('第一段第二行。'),label+': single newline split');
 assert(parts[0].querySelectorAll('p').length===3&&parts[0].textContent.includes('第三段。'),label+': paragraphs not combined');
 const images=[...row.querySelectorAll('.gallery')];
 assert(images.length===2&&images.every(g=>g.querySelectorAll('img').length===1&&!g.closest('.bubble,.bot-bubble')),label+': images not isolated');
 const quote=row.querySelectorAll('.quote-block');assert(quote.length===1&&quote[0]===row.firstElementChild.firstElementChild,label+': quote repeated or misplaced');
 const nodes=[...row.firstElementChild.children].filter(n=>n.matches('.bubble,.bot-bubble,.gallery'));
 for(let i=1;i<nodes.length;i++){const a=nodes[i-1].getBoundingClientRect(),b=nodes[i].getBoundingClientRect();assert(b.top>a.bottom,label+': bubbles touch or overlap')}
};
(async()=>{
 const transcript=JSON.stringify(fixture.messages[bots[0].name]);
 await waitFor(()=>$('[data-seq="'+splitUser.seq+'"] audio')&&$('#thread .msg.bot audio'),'mixed messages missing');
 const user=$('[data-seq="'+splitUser.seq+'"]'),bot=$('#thread .msg.bot');
 if(new URLSearchParams(location.search).has('shot')){document.body.dataset.testResult='ready';return}
 check(user,'user');check(bot,'bot');
 assert(JSON.stringify(fixture.messages[bots[0].name])===transcript,'projection changed transcript');
 user.querySelectorAll('.thumb')[1].click();await waitFor(()=>$('#lightbox')?.open,'image viewer');
 assert($('.image-count').textContent==='2 / 2','single image bubble lost message gallery');
 $('[aria-label="上一张"]').click();assert($('.image-count').textContent==='1 / 2','message image navigation lost');
 $('[aria-label="关闭"]').click();
 // CRLF and repeated empty paragraphs do not produce empty bubbles.
 splitUser.content='甲\r\n乙\r\n\r\n\r\n丙';splitUser.images=[];splitUser.audios=[];
 document.dispatchEvent(new Event('visibilitychange'));
 await waitFor(()=>bubbles($('[data-seq="'+splitUser.seq+'"]')).length===1,'CRLF paragraphs');
 const crlf=bubbles($('[data-seq="'+splitUser.seq+'"]'));assert(crlf[0].textContent.includes('甲')&&crlf[0].textContent.includes('乙')&&crlf[0].querySelectorAll('p').length===2,'CRLF single line lost');
 // Preserve blank lines inside existing Markdown code/math blocks.
 const fence=String.fromCharCode(96).repeat(3);
 splitUser.content=fence+'js\nconst a = 1;\n\nconst b = 2;\n'+fence+'\n\n$$x\n\n+y$$';
 document.dispatchEvent(new Event('visibilitychange'));
 await waitFor(()=>$('[data-seq="'+splitUser.seq+'"] .code-block'),'code missing');
 assert(bubbles($('[data-seq="'+splitUser.seq+'"]')).length===1,'split code/math message');
 assert($('[data-seq="'+splitUser.seq+'"] code').textContent.includes('1;\n\nconst'),'code blank line lost');
 splitUser.content='行内代码 '+String.fromCharCode(96)+'$$'+String.fromCharCode(96)+'。\n\n代码之后。';
 document.dispatchEvent(new Event('visibilitychange'));
 await waitFor(()=>$('[data-seq="'+splitUser.seq+'"]')?.textContent.includes('代码之后。'),'inline code update');
 assert(bubbles($('[data-seq="'+splitUser.seq+'"]')).length===1,'inline code swallowed paragraph boundary');
 const input=$('#input');input.textContent=splitText;input.dispatchEvent(new Event('input',{bubbles:true}));$('#composer').requestSubmit();
 await waitFor(()=>splitPayload&&$('#thread .optimistic'),'optimistic send missing');
 const pending=bubbles($('#thread .optimistic'));
 assert(pending.length===1&&pending[0].textContent.includes('第一段第二行。')&&pending[0].querySelectorAll('p').length===3,'optimistic paragraphs not combined immediately');
 assert(splitPayload.text===splitText,'send payload split or changed');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`
