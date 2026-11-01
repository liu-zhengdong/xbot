package web

import "testing"

// Independent UI acceptance. runTodoBrowser loads the shipped app unchanged;
// this case scripts only HTTP DTOs and records the resulting user requests.
// Backend execution/history is covered by the real-binary smoke cases.
func TestContinuableSubagentsAcceptanceBrowser(t *testing.T) {
	runTodoBrowser(t, continuableAcceptanceFixture, continuableAcceptanceDriver)
}

const continuableAcceptanceFixture = `
localStorage.removeItem('xbot-panel-open');localStorage.removeItem('xbot.side');
localStorage.removeItem('xbot-workspace-hidden');
bots.forEach(b=>b.pinned=false);
const childAcceptance={
 primary:bots[0].name,secondary:bots[1].name,
 childID:'sa_acce000000000001',lateID:'sa_acce000000000002',
 title:'验收协作者甲',lateTitle:'验收延迟协作者',otherTitle:'验收协作者乙',
 routineTitle:'验收例程0830',posts:[],gets:[],profiles:{},holdPrimary:false,releaseOld:null,oldReturned:false
};
const childAcceptanceTime='2026-11-01T08:00:00Z';
const childAcceptanceEntry=(seq,role,content,extra={})=>({seq,time:childAcceptanceTime,message:{role,content},...extra});
const childAcceptanceText=text=>[{type:'text',text}];
const childAcceptanceTool=(id)=>[{type:'tool_call',tool_call:{id,type:'function',function:{name:'read_file',arguments:JSON.stringify({path:'acceptance-input.txt'})}}}];
const childAcceptanceRows=(marker,parent=childAcceptance.primary)=>[
 childAcceptanceEntry(1,'system',childAcceptanceText('本机协作者，只向直接父 Bot 交回结果。')),
 childAcceptanceEntry(2,'user',childAcceptanceText('读取验收文件并报告。'),{sender:'bot:'+parent}),
 childAcceptanceEntry(3,'assistant',childAcceptanceTool('acceptance-read')),
 childAcceptanceEntry(4,'tool',[{type:'tool_result',tool_call_id:'acceptance-read',text:marker}]),
 childAcceptanceEntry(5,'assistant',childAcceptanceText(marker+'_CONCLUSION'))
];
const childAcceptanceMeta=(id,task)=>({id,type:'dev',task,status:'done',mode:'brief',steps:1,summary:'已完成首轮核对。',continuable:true,turn_seq:2,result_path:'/scripted/agents/'+id+'/results/2.txt',started_at:childAcceptanceTime,finished_at:childAcceptanceTime});
const childAcceptanceData={
 [childAcceptance.primary]:[
  {meta:childAcceptanceMeta(childAcceptance.childID,childAcceptance.title),entries:childAcceptanceRows('ACCEPT_TOOL_OUTPUT_ALPHA')},
  {meta:childAcceptanceMeta(childAcceptance.lateID,childAcceptance.lateTitle),entries:childAcceptanceRows('ACCEPT_PRIMARY_LATE_RECORD')}
 ],
 // Reuse a child id under another Bot on purpose: (Bot,id), not id alone,
 // owns the selected conversation and every late reply/action closure.
 [childAcceptance.secondary]:[
  {meta:childAcceptanceMeta(childAcceptance.lateID,childAcceptance.otherTitle),entries:childAcceptanceRows('ACCEPT_TOOL_OUTPUT_BETA',childAcceptance.secondary)}
 ]
};
const childAcceptanceBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>{
 const route=String(path),url=new URL(route,location.href),method=(opts.method||'GET').toUpperCase();
 if(url.pathname==='/api/bots/status')return json({bots:bots.map(b=>({bot:b.name,state:'idle',agents:childAcceptanceData[b.name].filter(c=>c.meta.status==='running').length,last_message:'验收对话'}))});
 const detail=url.pathname.match(/^\/api\/bots\/([^/]+)$/);
 if(detail&&method==='GET'){
  const name=decodeURIComponent(detail[1]);childAcceptance.profiles[name]=(childAcceptance.profiles[name]||0)+1;
  return json({name,links:[],agents:childAcceptanceData[name].map(c=>({...c.meta})),routines:[{id:'accept-routine',name:childAcceptance.routineTitle,enabled:true,cron:'30 8 * * *'}]});
 }
 const match=url.pathname.match(/^\/api\/bots\/([^/]+)\/agents\/([^/]+)\/(transcript|messages|stop)$/);
 if(!match)return childAcceptanceBaseFetch(path,opts);
 const name=decodeURIComponent(match[1]),id=decodeURIComponent(match[2]),kind=match[3];
 const child=(childAcceptanceData[name]||[]).find(c=>c.meta.id===id);
 if(!child)return new Response('{"error":"not found"}',{status:404,headers:{'Content-Type':'application/json'}});
 if(kind==='transcript'&&method==='GET'){
  childAcceptance.gets.push({name,id});
  const before=Number(url.searchParams.get('before')||Infinity),limit=Math.min(200,Number(url.searchParams.get('limit')||40));
  const eligible=child.entries.filter(e=>e.seq<before),entries=eligible.slice(-limit);
  const snapshot=JSON.parse(JSON.stringify({id,entries,has_more:eligible.length>entries.length,next_before:entries[0]?.seq,turn_seq:child.meta.turn_seq,status:child.meta.status,continuable:true}));
  if(childAcceptance.holdPrimary&&name===childAcceptance.primary&&id===childAcceptance.lateID){
   childAcceptance.holdPrimary=false;
   snapshot.entries.push(childAcceptanceEntry(snapshot.entries.at(-1).seq+1,'assistant',childAcceptanceText('ACCEPT_STALE_PRIMARY_REPLY')));
   await new Promise(resolve=>childAcceptance.releaseOld=resolve);
   childAcceptance.oldReturned=true;
  }
  return json(snapshot);
 }
 if((kind==='messages'||kind==='stop')&&method==='POST'){
  const body=JSON.parse(opts.body||'{}');childAcceptance.posts.push({name,id,kind,body});
  if(kind==='messages'){
   const seq=child.entries.at(-1).seq+1;
   child.meta.status='running';child.meta.finished_at=null;child.meta.turn_seq=seq;
   child.entries.push(childAcceptanceEntry(seq,'user',childAcceptanceText(body.text),{kind:'message_received',sender:'owner',turn_seq:seq}));
   return json({id,status:'running',turn_seq:seq});
  }
  child.meta.status='cancelled';child.meta.finished_at=childAcceptanceTime;
  child.meta.summary='这一轮已停止，原对话可以继续。';
  return json({id,status:'cancelled'});
 }
 return new Response('{"error":"method"}',{status:405,headers:{'Content-Type':'application/json'}});
};
`

const continuableAcceptanceDriver = `
const $=selector=>document.querySelector(selector),sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const assert=(value,label)=>{if(!value)throw Error(label)};
const waitFor=async(predicate,label)=>{for(let i=0;i<1200;i++){if(predicate())return;await sleep(10)}throw Error('timed out: '+label)};
const visible=node=>!!node&&node.checkVisibility();
const tabs=()=>[...document.querySelectorAll('[role=tab]')].filter(visible);
const tab=name=>tabs().find(node=>node.textContent.trim()===name);
const panelFor=name=>document.getElementById(tab(name)?.getAttribute('aria-controls')||'');
const accessibleName=node=>node.getAttribute('aria-label')||node.textContent||'';
const action=(root,pattern)=>[...root.querySelectorAll('button,[role=button],summary')].filter(visible).find(node=>pattern.test(accessibleName(node)));
const rendered=(root,text)=>visible(root)&&root.innerText.includes(text);
const inputText=node=>'value' in node?node.value:node.textContent;
const typeText=(node,text)=>{
 if('value' in node)node.value=text;else node.textContent=text;
 node.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:text}));
};
const childEditor=root=>{
 const nodes=[...root.querySelectorAll('textarea,input[type=text],input:not([type]),[contenteditable=true]')].filter(visible);
 return nodes.find(node=>node.tagName==='TEXTAREA')||nodes.find(node=>/反馈|指示|消息/.test(node.getAttribute('aria-label')||node.getAttribute('placeholder')||''))||nodes.find(node=>node.isContentEditable);
};
const openEditor=async root=>{
 if(!childEditor(root)){
  const open=action(root,/继续|引导|追加|发消息/);assert(open,'selected child has no continuation input action');open.click();
 }
 await waitFor(()=>childEditor(root),'child continuation editor');return childEditor(root);
};
const selectTask=async(title,name,id)=>{
 const root=panelFor('子代理');
 const row=action(root,new RegExp(title));assert(row,'child cannot be selected through visible UI: '+title);row.click();
 await waitFor(()=>childAcceptance.gets.some(get=>get.name===name&&get.id===id),'selected child transcript request');
 return root;
};
const revealEvidence=async(root,marker)=>{
 const clicked=new WeakSet();
 for(let i=0;i<120;i++){
  if(rendered(root,'acceptance-input.txt')&&rendered(root,marker))return;
  for(const node of root.querySelectorAll('button,[role=button],summary')){
   if(!visible(node)||clicked.has(node))continue;
   const label=accessibleName(node);
   if(!/文件|读取|read_file|轨迹|记录|步骤|调用|工具|原文|原始|详情|任务信息|结果|输出/.test(label))continue;
   if(node.tagName==='SUMMARY'&&node.parentElement.open)continue;
   if(node.getAttribute('aria-expanded')==='true')continue;
   clicked.add(node);node.click();
  }
  await sleep(20);
 }
 throw Error('public child tool call and its result are not readable: '+marker);
};
const selectOutput=(root,marker)=>{
 const walker=document.createTreeWalker(root,NodeFilter.SHOW_TEXT);
 let node;
 while(node=walker.nextNode()){
  const at=node.data.indexOf(marker);if(at<0||!visible(node.parentElement))continue;
  const range=document.createRange();range.setStart(node,at);range.setEnd(node,at+marker.length);
  const selection=document.getSelection();selection.removeAllRanges();selection.addRange(range);return node;
 }
 throw Error('readable child tool output is not selectable');
};
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes(childAcceptance.primary)&&!$('.thread-loading-cover'),'initial Bot');
 await waitFor(()=>tab('聊天')&&tab('任务')&&tab('子代理'),'independent child Tab');
 assert(tab('任务')!==tab('子代理')&&tab('任务').parentElement===tab('子代理').parentElement,'children are nested under Routine instead of a peer Tab');
 const mainInput=$('#input'),mainDraft='MAIN_BOT_DRAFT_MUST_SURVIVE_CHILD_CONTROLS';
 typeText(mainInput,mainDraft);
 tab('任务').click();await waitFor(()=>rendered(panelFor('任务'),childAcceptance.routineTitle),'Routine remains reachable');
 assert(!panelFor('任务').innerText.includes(childAcceptance.title)&&!panelFor('任务').innerText.includes(childAcceptance.lateTitle),'Routine page still mixes child tasks');
 tab('子代理').click();await waitFor(()=>rendered(panelFor('子代理'),childAcceptance.title),'child list');
 assert(!panelFor('子代理').innerText.includes(childAcceptance.routineTitle),'child page mixes Routine settings');
 const primaryPanel=await selectTask(childAcceptance.title,childAcceptance.primary,childAcceptance.childID);
 await revealEvidence(primaryPanel,'ACCEPT_TOOL_OUTPUT_ALPHA');
 assert(childAcceptance.gets.some(get=>get.name===childAcceptance.primary&&get.id===childAcceptance.childID),'tool evidence came from a different child');
 assert(!$('#thread').innerText.includes('ACCEPT_TOOL_OUTPUT_ALPHA'),'child raw tool output leaked into main chat');
 let editor=await openEditor(primaryPanel);
 const feedback='OWNER_FEEDBACK_TO_ORIGINAL_CHILD';typeText(editor,feedback);
 const send=action(primaryPanel,/^发送给这个子代理$/);assert(send&&!send.disabled,'child input cannot be submitted');send.click();
 await waitFor(()=>childAcceptance.posts.length===1,'feedback POST');
 const post=childAcceptance.posts[0];
 assert(post.name===childAcceptance.primary&&post.id===childAcceptance.childID&&post.kind==='messages'&&post.body.text===feedback,'feedback was not posted to the original (Bot,id)');
 assert(post.body.delivery===undefined||post.body.delivery==='steer','default follow-up did not use steer delivery');
 assert(fixture.streams.length===0&&inputText(mainInput)===mainDraft,'child follow-up submitted or overwrote the main Bot draft');
 await waitFor(()=>{const button=action(primaryPanel,/停止/);return button&&!button.disabled},'running child Stop action');
 action(primaryPanel,/停止/).click();
 // The existing shared control may require a second visible click.
 const confirm=action(primaryPanel,/再点一次停止|确认停止/);if(confirm)confirm.click();
 await waitFor(()=>childAcceptance.posts.length===2,'Stop POST');
 assert(childAcceptance.posts[1].kind==='stop'&&childAcceptance.posts[1].name===childAcceptance.primary&&childAcceptance.posts[1].id===childAcceptance.childID,'Stop targeted a different child');
 await waitFor(()=>/已停止|已取消/.test(primaryPanel.innerText),'returned terminal state is shown');
 assert(inputText(mainInput)===mainDraft&&fixture.streams.length===0,'Stop altered the main Bot conversation');
 tab('聊天').click();assert($('#input')===mainInput&&inputText(mainInput)===mainDraft,'main draft did not survive child Tab/actions');
 tab('子代理').click();
 editor=await openEditor(primaryPanel);
 const childDraft='UNSENT_CHILD_DRAFT_AFTER_STOP';typeText(editor,childDraft);editor.focus();
 let selectionText='';
 if(editor.setSelectionRange)editor.setSelectionRange(3,11);
 else{const range=document.createRange();range.setStart(editor.firstChild,3);range.setEnd(editor.firstChild,11);document.getSelection().removeAllRanges();document.getSelection().addRange(range);selectionText=childDraft.slice(3,11)}
 const profileReads=childAcceptance.profiles[childAcceptance.primary]||0;
 await waitFor(()=>(childAcceptance.profiles[childAcceptance.primary]||0)>=profileReads+2,'two profile polls while editing');
 assert(editor.isConnected&&childEditor(primaryPanel)===editor&&inputText(editor)===childDraft,'poll replaced the child editor or its unsent draft');
 assert(document.activeElement===editor,'poll stole the child input focus');
 if(editor.setSelectionRange)assert(editor.selectionStart===3&&editor.selectionEnd===11,'poll changed input selection');
 else assert(document.getSelection().toString()===selectionText,'poll changed contenteditable selection');
 await revealEvidence(primaryPanel,'ACCEPT_TOOL_OUTPUT_ALPHA');
 // Reading tool output leaves the editor. Restrict the Range to the paired
 // result detail, rather than the model conclusion carrying the same marker.
 editor.blur();
 const evidence=primaryPanel.querySelector('aside[aria-label="记录详情"]');
 assert(visible(evidence),'selected tool detail is not visible');
 const outputNode=selectOutput(evidence,'ACCEPT_TOOL_OUTPUT_ALPHA'),selectionReads=childAcceptance.profiles[childAcceptance.primary]||0;
 assert(document.getSelection().toString()==='ACCEPT_TOOL_OUTPUT_ALPHA','fixture did not establish the intended tool-result selection');
 await waitFor(()=>(childAcceptance.profiles[childAcceptance.primary]||0)>=selectionReads+2,'two polls with selected tool output');
 assert(outputNode.isConnected&&document.getSelection().toString()==='ACCEPT_TOOL_OUTPUT_ALPHA','poll replaced selected child tool evidence');
 assert(inputText(editor)===childDraft,'record polling cleared the unsent child draft');
 document.getSelection().removeAllRanges();
 // Hold a real child-transcript fetch at the HTTP seam, then switch Bot.
 const back=action(primaryPanel,/^子代理$/);assert(back,'child conversation has no way back to its sibling list');back.click();
 await waitFor(()=>action(primaryPanel,new RegExp(childAcceptance.lateTitle)),'return to child list');
 childAcceptance.holdPrimary=true;
 const lateRow=action(primaryPanel,new RegExp(childAcceptance.lateTitle));assert(lateRow,'second child missing');lateRow.click();
 await waitFor(()=>childAcceptance.releaseOld,'held old-Bot child transcript response');
 document.querySelector('[data-bot="'+childAcceptance.secondary+'"]').click();
 await waitFor(()=>$('#title').textContent.includes(childAcceptance.secondary)&&!$('.thread-loading-cover'),'second Bot');
 tab('子代理').click();await waitFor(()=>rendered(panelFor('子代理'),childAcceptance.otherTitle),'second Bot child list');
 const secondaryPanel=await selectTask(childAcceptance.otherTitle,childAcceptance.secondary,childAcceptance.lateID);
 await revealEvidence(secondaryPanel,'ACCEPT_TOOL_OUTPUT_BETA');
 const otherEditor=await openEditor(secondaryPanel);
 assert(inputText(otherEditor)==='','another Bot inherited the first child draft');
 childAcceptance.releaseOld();await waitFor(()=>childAcceptance.oldReturned,'old response delivered');await sleep(100);
 assert($('#title').textContent.includes(childAcceptance.secondary)&&tab('子代理').getAttribute('aria-selected')==='true','late child response changed current Bot or page');
 assert(rendered(secondaryPanel,'ACCEPT_TOOL_OUTPUT_BETA')&&!secondaryPanel.innerText.includes('ACCEPT_STALE_PRIMARY_REPLY')&&!secondaryPanel.innerText.includes('ACCEPT_PRIMARY_LATE_RECORD'),'late child history crossed Bot ownership');
 typeText(otherEditor,'SECOND_BOT_ONLY_FEEDBACK');
 const otherSend=action(secondaryPanel,/^发送给这个子代理$/);assert(otherSend,'second child input missing');otherSend.click();
 await waitFor(()=>childAcceptance.posts.length===3,'second Bot feedback');
 assert(childAcceptance.posts[2].name===childAcceptance.secondary&&childAcceptance.posts[2].id===childAcceptance.lateID&&childAcceptance.posts[2].body.text==='SECOND_BOT_ONLY_FEEDBACK','late response retargeted the continuation action to the old Bot');
 assert(fixture.streams.length===0,'child action was sent as a main Bot chat message');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(error=>{document.body.dataset.testResult='failed: '+error.message});
`
