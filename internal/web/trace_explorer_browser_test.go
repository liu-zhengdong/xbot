package web

import "testing"

// Public transcript windows drive the shipped app; no private JS functions are called.
func TestTraceExplorerBrowser(t *testing.T) {
	runTodoBrowser(t, traceExplorerFixture, traceExplorerSetup+traceExplorerDriver)
}

func TestTraceExplorerReadingBrowser(t *testing.T) {
	runTodoBrowser(t, traceExplorerFixture, traceExplorerSetup+traceExplorerReadingDriver)
}

const traceExplorerFixture = `
localStorage.removeItem('xbot.side');bots.forEach(b=>b.pinned=false);
const traceFixture={requests:[],version:1,hold:false,release:null,copies:[],copyResolve:null};
const traceCall=(id,name,args)=>({id,type:'function',function:{name,arguments:JSON.stringify(args)}});
fixture.messages[bots[0].name]=Array.from({length:100},(_,i)=>({seq:i+1,role:'user',content:'较早输入 '+(i+1),time:'2026-11-01T08:00:00Z'}));
fixture.messages[bots[0].name][19]={seq:20,role:'assistant',content:'',tool_calls:[traceCall('outside-window','read_file',{path:'outside.txt'})],time:'2026-11-01T08:00:00Z'};
fixture.messages[bots[0].name][98]={seq:99,role:'user',content:'内核无外壳上下文 INTERNAL',internal:true,time:'2026-11-01T08:00:00Z'};
fixture.seq=100;
const tracePush=(role,text,extra={})=>{const m={...message(role,text),...extra};fixture.messages[bots[0].name].push(m);return m};
tracePush('user','验收输入：检查资料中的 ORCHID 正文');
tracePush('user','<environment_update>工作区上下文标记</environment_update>');
tracePush('assistant','模型正文 ORCHID，接下来分别读文件与核对失败。',{thinking:'按顺序核对资料，思考不应进入聊天',tool_calls:[traceCall('read-one','read_file',{path:'one.txt'}),traceCall('fail-two','shell',{command:'false',description:'核对失败路径'})]});
tracePush('tool','文件结果独有 LANTERN，第一份资料。',{name:'read_file',tool_call_id:'read-one'});
tracePush('tool','error: 核对失败 COBALT',{name:'shell',tool_call_id:'fail-two'});
tracePush('assistant','',{parts:[{type:'server_tool',text:'[厂商生图: 已保存到工作区 generated/trace.png]',hosted:{id:'hosted-one',name:'image_generation',status:'completed',query:'安静的海边'}}]});
tracePush('assistant','阅读正文 SELECTABLE。\n'+Array.from({length:100},(_,i)=>'正文长行 '+i+'，轮询时保留正在阅读的选区和滚动。').join('\n'));
tracePush('tool','窗口孤立结果 AMBER',{name:'read_file',tool_call_id:'outside-window'});
fixture.messages[bots[1].name]=[{seq:1,role:'user',content:'另一个 Bot 的独立输入',time:'2026-11-01T08:00:00Z'}];
Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:text=>{traceFixture.copies.push(text);return new Promise(r=>traceFixture.copyResolve=r)}}});
document.execCommand=()=>false;
const traceBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const u=new URL(String(path),'http://fixture.local'),match=u.pathname.match(/^\/api\/bots\/([^/]+)\/transcript$/);
 if(!match)return traceBaseFetch(path,opts);
 const name=decodeURIComponent(match[1]),q=u.searchParams,all=fixture.messages[name]||[];
 const before=Number(q.get('before')||0),after=Number(q.get('after')||0),limit=Number(q.get('limit')||60);
 traceFixture.requests.push({name,before,after,limit});
 let messages=all;
 if(before)messages=all.filter(m=>m.seq<before).slice(-limit);
 else if(after)messages=all.filter(m=>m.seq>after).slice(0,limit);
 else messages=all.slice(-limit);
 const head=messages[0]?.seq||0,tail=messages.at(-1)?.seq||0,total=all.at(-1)?.seq||0;
 const tag='W/"trace-'+encodeURIComponent(name)+'-'+traceFixture.version+'-'+u.search+'"';
 const body=JSON.stringify({messages,window:{head_seq:head,tail_seq:tail,total,next_before:head,next_after:tail,has_more_before:head>1,has_more_after:tail<total}});
 if(traceFixture.hold&&name===bots[0].name){traceFixture.hold=false;await new Promise(r=>traceFixture.release=r)}
 if(new Headers(opts.headers).get('If-None-Match')===tag)return new Response(null,{status:304,headers:{ETag:tag}});
 return new Response(body,{headers:{'Content-Type':'application/json',ETag:tag}});
};
`

// Shared DOM vocabulary also keeps the migrated mixed-behaviour tests on the same entrance.
const traceExplorerSetup = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1200;i++){if(fn())return;await sleep(10)}throw Error(msg)};
const traceRows=()=>[...document.querySelectorAll('button.trace-event-row[data-event-key][data-seq]')].filter(n=>!n.hidden);
const traceSelected=n=>n?.getAttribute('aria-pressed')==='true'||n?.getAttribute('aria-selected')==='true';
const traceDetail=()=>$('aside[aria-label="记录详情"]');
const traceButton=(host,name)=>[...host.querySelectorAll('button')].find(n=>{const label=(n.getAttribute('aria-label')||n.textContent).trim();return label===name||label.replace(/\s*\d+\s*$/,'').trim()===name});
const traceTab=async name=>{const n=traceButton($('nav[aria-label="详情视图"]'),name);assert(n,'missing detail tab '+name);n.click();await sleep(20)};
const traceOpen=async row=>{assert(row,'event missing');row.click();await waitFor(()=>traceDetail()&&traceDetail().getClientRects().length,'detail did not open')};
const traceClose=async()=>{traceButton(traceDetail(),'关闭详情').click();await waitFor(()=>!traceDetail()||!traceDetail().getClientRects().length,'detail did not close')};
const openTrace=async()=>{
 await waitFor(()=>!$('#shell').hidden&&!$('.thread-loading-cover')&&$('#workspace-tabs [role=tab]'),'app did not boot');
 [...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent==='轨迹').click();
 await waitFor(()=>$('.trace-explorer')&&traceRows().length,'trace did not load');
};
const traceSearch=async value=>{const n=$('input[aria-label="搜索轨迹"]');n.value=value;n.dispatchEvent(new Event('input',{bubbles:true}));await sleep(40)};
const traceScroller=node=>{for(let n=node;n;n=n.parentElement){if(/auto|scroll/.test(getComputedStyle(n).overflowY)&&n.scrollHeight>n.clientHeight+5)return n}throw Error('no independent scroll region')};
`

const traceExplorerDriver = `
(async()=>{
 await openTrace();
 const at=seq=>traceRows().filter(n=>Number(n.dataset.seq)===seq);
 assert(at(101).length===1&&at(102).length===1,'input or kernel context missing');
 assert(at(103).length===3,'model and multiple calls are not independently selectable');
 assert(!at(104).length&&!at(105).length,'associated results duplicated as unrelated events');
 assert(at(108).length===1,'window-isolated result disappeared');
 const seqs=traceRows().map(n=>Number(n.dataset.seq));assert(seqs.every((n,i)=>!i||n>=seqs[i-1]),'events are not in transcript order');
 await traceOpen(at(102)[0]);assert(traceDetail().textContent.includes('上下文'),'kernel context classified as owner input');await traceClose();
 await traceOpen(at(99)[0]);assert(traceDetail().textContent.includes('上下文'),'explicit internal record classified as owner input');await traceClose();
 const workspaceTrace=[...document.querySelectorAll('#workspace-tabs [role=tab]')].find(n=>n.textContent==='轨迹');
 workspaceTrace.focus();workspaceTrace.dispatchEvent(new KeyboardEvent('keydown',{key:'f',ctrlKey:true,bubbles:true,cancelable:true}));
 assert(document.activeElement===$('input[aria-label="搜索轨迹"]'),'Ctrl+F from page tab did not focus trace search');
 const read=at(103).find(n=>n.textContent.includes('read_file')),readKey=read.dataset.eventKey;
 await traceOpen(read);await traceTab('预览');
 assert(traceDetail().textContent.includes('LANTERN')&&!traceDetail().textContent.includes('COBALT'),'tool results associated by position rather than call id');
 await traceTab('原始内容');assert(traceDetail().textContent.includes('read-one')&&traceDetail().textContent.includes('one.txt'),'raw call missing');
 await traceTab('来源');assert(traceDetail().textContent.includes('103')&&traceDetail().textContent.includes('104'),'source lost call/result sequence');
 await traceTab('概述');
 const overview=$('[aria-label="轨迹总览"]');assert(overview,'overview missing');
 const markers=[...overview.querySelectorAll('button[data-event-key]')].filter(n=>n.dataset.eventKey===readKey);
 assert(markers.length>=2,'overview lost associated result marker');
 await traceClose();markers.at(-1).click();await waitFor(()=>traceDetail()?.textContent.includes('read_file'),'result overview marker did not open call');
 assert(traceSelected(traceRows().find(n=>n.dataset.eventKey===readKey)),'overview and selected row disagree');
 await traceTab('原始内容');traceButton(traceDetail(),'复制原文').click();
 await waitFor(()=>traceFixture.copies.length===1,'copy entry did not execute clipboard write');
 assert(traceFixture.copies[0].includes('read-one')&&traceFixture.copies[0].includes('LANTERN'),'copied record omitted public call or result');
 assert(!$('#toast')?.textContent.includes('已复制'),'copy success appeared before write settled');
 traceFixture.copyResolve();await waitFor(()=>$('#toast')?.textContent.includes('已复制'),'successful copy has no feedback');
 await traceClose();
 await traceSearch('oRcHiD');assert(at(101).length===1&&at(103).length===1,'body search is not literal case-insensitive matching');
 await traceSearch('LANTERN');assert(traceRows().length===1&&traceRows()[0].dataset.eventKey===readKey,'result body is not searchable through call');
 await traceSearch('AMBER');assert(traceRows().length===1&&Number(traceRows()[0].dataset.seq)===108,'orphan result body is not searchable');
 await traceOpen(traceRows()[0]);await traceTab('预览');assert(traceDetail().textContent.includes('AMBER'),'orphan result not readable');await traceClose();
 await traceSearch('没有这段内容');assert(traceRows().length===0&&$('.trace-explorer').textContent.includes('匹配'),'no-match search retained rows');
 await traceSearch('');
 const type=$('[aria-label="事件类型"]');assert(type,'type selector absent');
 const selectType=async label=>{if(type.tagName==='SELECT'){const o=[...type.options].find(o=>o.textContent.trim()===label);assert(o,'type option missing '+label);type.value=o.value;type.dispatchEvent(new Event('change',{bubbles:true}))}else{traceButton(type,label).click()}await sleep(30)};
 await selectType('输入');assert(traceRows().length>0&&traceRows().every(n=>!n.textContent.includes('read_file')&&!n.textContent.includes('上下文')),'type filter kept other kinds');
 await selectType('上下文');assert(traceRows().length===2&&traceRows().every(n=>[99,102].includes(Number(n.dataset.seq))),'context type filter lost explicit internal or wrapped context');
 await selectType('工具');assert(traceRows().length===4,'tool filter lost local, hosted or orphan event');
 const error=traceButton($('.trace-explorer'),'错误')||traceButton($('.trace-explorer'),'只看错误');assert(error,'error filter missing');error.click();await sleep(30);
 assert(traceRows().length===1&&traceRows()[0].textContent.includes('shell'),'error filter did not isolate failed call');
 await selectType('全部');
 await traceOpen(at(107)[0]);await traceTab('预览');
 workspaceTrace.focus();workspaceTrace.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));await waitFor(()=>!traceDetail()||!traceDetail().getClientRects().length,'Escape from page tab did not close detail');
 await traceOpen(at(101)[0]);await traceClose();
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`

const traceExplorerReadingDriver = `
(async()=>{
 await openTrace();
 await traceSearch('阅读正文');
 const selected=traceRows().find(n=>Number(n.dataset.seq)===107);await traceOpen(selected);await traceTab('预览');
 const detail=traceDetail(),body=[...detail.querySelectorAll('pre,p')].find(n=>n.textContent.includes('SELECTABLE'));
 assert(body,'model body missing');
 const scroller=traceScroller(body);scroller.scrollTop=130;await sleep(20);const top=scroller.scrollTop;
 const query=$('input[aria-label="搜索轨迹"]');query.focus();query.setSelectionRange(2,4);
 // Keep input focus/caret and document Selection in separate native phases.
 const reads=traceFixture.requests.length;
 tracePush('user','追加输入不应影响阅读');traceFixture.version++;
 await waitFor(()=>traceFixture.requests.length>=reads+2,'two trace polls missing');
 assert(query===$('input[aria-label="搜索轨迹"]')&&document.activeElement===query&&query.value==='阅读正文'&&query.selectionStart===2&&query.selectionEnd===4,'poll reset search, caret or focus');
 assert(traceDetail()===detail&&selected.isConnected&&traceSelected(selected),'poll replaced selected detail');
 assert(traceSelected(traceButton($('nav[aria-label="详情视图"]'),'预览')),'poll reset detail tab');
 assert(Math.abs(scroller.scrollTop-top)<3,'poll changed detail scroll');
 query.blur();
 const walker=document.createTreeWalker(body,NodeFilter.SHOW_TEXT);let text;while(walker.nextNode()){if(walker.currentNode.textContent.includes('SELECTABLE')){text=walker.currentNode;break}}
 const range=document.createRange(),start=text.textContent.indexOf('SELECTABLE');range.setStart(text,start);range.setEnd(text,start+10);getSelection().removeAllRanges();getSelection().addRange(range);
 const selectionReads=traceFixture.requests.length;tracePush('user','选择正文期间继续追加');traceFixture.version++;
 await waitFor(()=>traceFixture.requests.length>=selectionReads+2,'two selection polls missing');
 assert(getSelection().toString()==='SELECTABLE'&&getSelection().anchorNode===text,'poll lost body selection');
 assert(Math.abs(scroller.scrollTop-top)<3,'selection poll changed detail scroll');
 getSelection().removeAllRanges();await traceClose();await traceSearch('');
 const anchor=traceRows().find(n=>Number(n.dataset.seq)===75),list=traceScroller(anchor);anchor.scrollIntoView({block:'center'});await sleep(30);const y=anchor.getBoundingClientRect().top;
 const earlier=traceButton($('.trace-explorer'),'更早的记录');assert(earlier,'before paging entry absent');earlier.click();
 await waitFor(()=>traceFixture.requests.some(r=>r.before)&&traceRows().some(n=>Number(n.dataset.seq)===20),'before page not merged');await sleep(30);
 assert(anchor.isConnected&&Math.abs(anchor.getBoundingClientRect().top-y)<4,'before prepend moved reading anchor');
 assert(list.scrollTop>0,'before prepend jumped to list start');
 // A result that was isolated in the tail must join its call after the before page arrives.
 const outside=traceRows().find(n=>Number(n.dataset.seq)===20&&n.textContent.includes('read_file'));await traceOpen(outside);await traceTab('预览');assert(traceDetail().textContent.includes('AMBER'),'cross-window result did not join earlier call');await traceClose();
 assert(!traceRows().some(n=>Number(n.dataset.seq)===108),'merged orphan remained duplicated');
 // Hold a real poll response across selection change, then release it late.
 await traceSearch('AMBER');await traceOpen(outside);await traceTab('来源');
 traceFixture.hold=true;await waitFor(()=>traceFixture.release,'held poll did not start');
 $('[data-bot="'+bots[1].name+'"]').click();await waitFor(()=>$('#title').textContent.includes(bots[1].name)&&!$('.thread-loading-cover'),'other Bot chat did not load');
 await openTrace();await waitFor(()=>traceRows().some(n=>n.textContent.includes('独立输入')),'other Bot trace did not load');
 traceFixture.release();await sleep(50);
 assert(!$('input[aria-label="搜索轨迹"]').value&&(!traceDetail()||!traceDetail().getClientRects().length),'trace selection/query leaked to another Bot');
 assert(!$('.trace-explorer').textContent.includes('AMBER')&&!$('.trace-explorer').textContent.includes('SELECTABLE'),'late trace response crossed Bot boundary');
 assert(testErrors.length===0,testErrors.join('; '));document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
