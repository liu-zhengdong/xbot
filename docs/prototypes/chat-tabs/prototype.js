'use strict';
const $=s=>document.querySelector(s), $$=s=>[...document.querySelectorAll(s)];
const shapes={
chat:'<path d="M21 11.5a8.4 8.4 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.4 8.4 0 0 1-3.8-.9L3 21l1.9-5.7A8.4 8.4 0 0 1 4 11.5a8.5 8.5 0 0 1 4.7-7.6 8.4 8.4 0 0 1 3.8-.9h.5a8.5 8.5 0 0 1 8 8z"/>',
brain:'<path d="M12 18V5a3 3 0 0 0-5.8-1 4 4 0 0 0-3.3 6.2 4 4 0 0 0 .6 7.2A4 4 0 1 0 12 18Zm0 0V5a3 3 0 0 1 5.8-1 4 4 0 0 1 3.3 6.2 4 4 0 0 1-.6 7.2A4 4 0 1 1 12 18Z"/><path d="M8 8c0 2-1 3-3 3m14 0c-2 0-3-1-3-3M7 15c2 0 3 1 3 3m4 0c0-2 1-3 3-3"/>',
trace:'<circle cx="6" cy="5" r="2"/><circle cx="18" cy="6" r="2"/><circle cx="6" cy="19" r="2"/><path d="M6 7v10m2-3h5a5 5 0 0 0 5-5V8"/>',
task:'<path d="m3 6 2 2 4-4m-6 9 2 2 4-4m-6 9 2 2 4-4M13 6h8m-8 7h8m-8 7h8"/>',
context:'<path d="m12 3 10 5-10 5L2 8Zm-10 9 10 5 10-5M2 16l10 5 10-5"/>',
inbox:'<path d="M4 4h16l2 10v6H2v-6Zm-2 10h6l2 3h4l2-3h6"/>',
more:'<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
plus:'<path d="M12 5v14M5 12h14"/>',search:'<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',
side:'<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>',down:'<path d="m6 9 6 6 6-6"/>',
arrow:'<path d="M12 19V5m-6 6 6-6 6 6"/>',check:'<path d="m5 12 4 4L19 6"/>',
file:'<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Zm0 0v6h6M8 13h8m-8 4h6"/>',
copy:'<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V4H4v12h4"/>',
sun:'<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/>',
mic:'<rect x="9" y="2" width="6" height="12" rx="3"/><path d="M5 10v2a7 7 0 0 0 14 0v-2m-7 9v3m-4 0h8"/>',
settings:'<path d="m12 3 2 3 3-.2.8 3 2.7 1.5L19 13l.5 3-3 1-1.5 3-3-1-3 1-1.5-3-3-1 .5-3-1.5-2.7L6.7 9l.8-3 3 .2Z"/><circle cx="12" cy="12" r="3"/>',
clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
external:'<path d="M14 3h7v7m0-7L10 14m0-11H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5"/>'
};
const icon=(name,cls='')=>`<svg class="${cls}" viewBox="0 0 24 24" aria-hidden="true">${shapes[name]||shapes.chat}</svg>`;
const btn=(name,label,action)=>`<button class="icon-btn" aria-label="${label}" data-action="${action}">${icon(name)}</button>`;
const tabs=[{id:'chat',label:'聊天',icon:'chat'},{id:'memory',label:'记忆',icon:'brain'},{id:'trace',label:'轨迹',icon:'trace'},{id:'tasks',label:'任务',icon:'task'},{id:'context',label:'上下文',icon:'context'},{id:'inbox',label:'收件箱',icon:'inbox'}];
const bots=[['XBot 主管','把聊天页面做得更舒服','chat'],['开发助手','设置页的滚动已修复','task'],['研究员','整理好了这周的阅读笔记','search'],['生活助手','明早 8 点提醒你','sun']];
const user=(text,time)=>`<div class="message user"><div class="user-text">${text}</div><div class="message-meta">${time}</div></div>`;
const assistant=(text,time)=>`<div class="message assistant"><div class="speaker"><span class="mini-avatar">${icon('chat')}</span><b class="bot-speaker">XBot 主管</b><span>${time}</span></div><div class="assistant-body">${text}</div><button class="icon-btn copy-message" aria-label="复制回复">${icon('copy')}</button></div>`;
const heading=(en,title,description,right='')=>`<div class="view-heading"><div><div class="eyebrow">${en}</div><h2>${title}</h2><p>${description}</p></div>${right}</div>`;
const quiet=(name,label,action)=>`<button class="quiet-button" data-action="${action}">${icon(name)}${label}</button>`;
const views={};
views.chat=`<div class="messages" id="messages"><div class="thread"><div class="date-label">今天 · 11 月 1 日</div>
${user('我想把聊天页面做得更舒服，顶部可以切换聊天、记忆和轨迹。','10:55')}
${assistant('<p>可以。把一个 Bot 的不同内容放进同一排标签里，找起来会更直接。</p><p>聊天放在第一项，平时打开就能继续说话；想看看我记住了什么，或者刚才做了哪些事，切一下就好。</p>','10:55')}
${user('对，先给我画一个原型，整体简洁一些，留白多一点。','10:57')}
${assistant('<p>这版把常用内容铺在顶部，主体留给当前正在看的内容。</p><p>你可以点上面的 <button class="inline-link" data-goto="memory">记忆</button> 和 <button class="inline-link" data-goto="trace">轨迹</button> 看看切换后的样子，也可以在「更多」里调整显示的标签。</p>','10:57')}
</div></div><div class="composer-area"><form class="composer" id="composer"><textarea id="draft" aria-label="消息" placeholder="发消息，继续这件事…" rows="2"></textarea>
<div class="composer-toolbar"><div class="composer-left">${btn('plus','添加附件','attach')}<span class="composer-tip">/ 调用指令 · @ 引用</span></div><div class="composer-right"><button type="button" class="model-button" data-action="model">GPT-5 ${icon('down')}</button>${btn('mic','语音输入','voice')}<button class="send" aria-label="发送消息" disabled>${icon('arrow')}</button></div></div></form>
<div class="composer-footer"><span>${icon('context')}5.8K / 128K 上下文</span><span>Enter 发送 · Shift + Enter 换行</span></div></div>`;
views.memory=heading('MEMORY','记忆','在持续的相处里，留下值得记住的事。',quiet('external','打开记忆文件','memory'))+`
<div class="memory-layout"><div><article class="memory-block"><div class="section-title">${icon('brain')}<h3>关于你</h3><span>与你的 Bot 共享</span></div>
<div class="memory-prose"><h4>政东</h4><p>你正在开发 XBot，希望它是一个能长期协作、有记忆的个人助手。</p><h4>协作偏好</h4><ul><li>用中文交流，先说结论，表达直接。</li><li>界面简洁，留白充足，避免不必要的装饰。</li><li>涉及 UI 的调整，先看原型，再确定实现。</li></ul><h4>工作方式</h4><p>一次推进一件明确的事，尽量复用已有能力。验证实际行为，再判断是否完成。</p></div>
<div class="block-foot">${icon('file')}个人记忆<span>更新于今天 09:42</span></div></article>
<article class="memory-block secondary-block"><div class="section-title">${icon('chat')}<h3>关于这个 Bot</h3><span>只属于 XBot 主管</span></div><div class="memory-prose"><p>负责 XBot 的产品方向、任务协调和开发进度。把讨论整理成明确的下一步，保持设计与实际行为一致。</p></div></article></div>
<aside class="experiences"><h3>最近的经历</h3><p class="muted">一起做过的事，按时间留下来。</p>
<details open><summary><span>今天 <small>10:57</small></span><b>重新组织聊天页面</b></summary><p>把聊天、记忆和轨迹放到顶部，先探索可扩展的页面结构。</p></details>
<details><summary><span>昨天 <small>18:30</small></span><b>调整设置页的阅读体验</b></summary><p>内容区域独立滚动，标题固定，方便浏览较长的设置内容。</p></details>
<details><summary><span>10 月 30 日</span><b>完善长期记忆的展示</b></summary><p>整篇呈现关于你和关于 Bot 的记忆，经历单独按日期展示。</p></details>
${quiet('file','打开本月流水','log')}</aside></div>`;
const round=(glyph,title,sub,body,open=false,status='已完成')=>`<details class="trace-round" ${open?'open':''}><summary><span class="round-glyph">${icon(glyph)}</span><span class="round-title"><b>${title}</b><small>${sub}</small></span><span class="round-status">${icon('check')}${status}</span>${icon('down','detail-chevron')}</summary><div class="trace-steps">${body}</div></details>`;
const step=(name,title,sub,time,body)=>`<details class="trace-step"><summary>${icon(name)}<span>${title}<small>${sub}</small></span><time>${time}</time>${icon('down','detail-chevron')}</summary><pre>${body}</pre></details>`;
views.trace=heading('ACTIVITY','轨迹','每一轮做了什么，都有迹可循。','<span class="quiet-label">今天 · 2 轮对话</span>')+'<div class="trace-date">今天，11 月 1 日</div>'+
round('trace','给聊天页面画一个原型','10:57 · 读取文件、生成原型 · 用时 42 秒',
step('file','查看现有聊天界面','了解页面结构和视觉规范','10:57:02','读取：聊天页面、视觉变量、记忆和轨迹面板说明。\n结果：沿用现有内容，把页面入口移到顶部。')+
step('context','整理页面内容','聊天、记忆、轨迹与更多标签','10:57:11','默认页面：聊天。\n内容区域：各标签使用同一个主区域。\n扩展入口：更多菜单，可收起不常用的标签。')+
step('file','生成交互原型','支持标签切换与深浅色预览','10:57:42','已生成本地 HTML 原型。\n这是演示数据，不会执行工具或调用模型。'),true)+
round('chat','讨论聊天页面的布局方向','10:55 · 回复消息 · 用时 8 秒','<p>明确以顶部标签承载 Bot 的不同内容，聊天优先，布局保持简洁。</p>')+'<div class="end-note">已显示今天的全部轨迹</div>';
views.tasks=heading('TASKS','任务','查看正在推进的事和最近的结果。')+'<h3 class="list-heading">最近结束</h3>'+
round('check','聊天页面原型 <em class="type-tag">开发</em>','今天 10:57 · 已完成','<p>已生成顶部标签式原型，可浏览聊天、记忆和轨迹页面。</p>')+
`<h3 class="list-heading">例程</h3><div class="routine"><span class="round-glyph">${icon('clock')}</span><span><b>每日进度回顾</b><small>每天 18:00 · 汇总当天进展</small></span><button role="switch" aria-checked="true" class="switch" aria-label="每日进度回顾"></button></div>`;
const segments=[['turn','本轮对话','1.8K','1.4%'],['hist','早先对话','1.4K','1.1%'],['sys','系统提示词','1.0K','.8%'],['mem','记忆','0.6K','.5%'],['tools','工具定义','1.0K','.7%']];
views.context=heading('CONTEXT','上下文','这一轮，我带着哪些信息来理解你。','<span class="quiet-label">最新一次请求</span>')+
`<div class="context-card"><div class="context-total"><b>5.8K <span>/ 128K</span></b><span>约 4.5% 已用</span></div><div class="context-band">${segments.map(s=>`<i style="--segment:var(--x-ctx-${s[0]});--part:${s[3]}"></i>`).join('')}</div><div class="legend">${segments.map(s=>`<div><i style="--segment:var(--x-ctx-${s[0]})"></i>${s[1]}<span>${s[2]}</span></div>`).join('')}</div><p class="muted">离后台整理还有约 90K，当前上下文充足。</p></div>`;
views.inbox=heading('INBOX','收件箱','从例程、文件和其他 Bot 到来的消息。','<span class="quiet-label">按时间排序</span>')+
round('clock','每日进度回顾','例程 · 昨天 18:00','<p>汇总今天的开发进度、完成的验证和仍需要跟进的事项。</p>',false,'已处理')+
round('file','项目说明有更新','文件监听 · 昨天 16:24','<p>项目文档发生变化，下次继续工作时读取最新内容。</p>',false,'已处理');
$('#app').innerHTML=`<div class="app"><aside class="sidebar" aria-label="Bot 列表"><div class="brand-row"><strong class="brand">XBot<span>.</span></strong>${btn('side','收起侧栏','sidebar')}</div>
<button class="side-search" data-action="search">${icon('search')}<span>查找 Bot</span><kbd>⌘ K</kbd></button><div class="side-label">我的 Bot ${btn('plus','新建 Bot','new')}</div>
<nav class="bot-list">${bots.map((b,i)=>`<button class="bot ${i===0?'active':''}" data-bot="${b[0]}"><span class="avatar">${icon(b[2])}</span><span><b>${b[0]}</b><small>${b[1]}</small></span>${i===0?'<i class="online" title="在线"></i>':''}</button>`).join('')}</nav>
<div class="sidebar-bottom"><div class="local"><i class="online"></i>本机已连接</div><button class="profile" data-action="profile"><span class="owner-avatar">政</span><span>政东<small>个人工作区</small></span>${icon('settings')}</button></div></aside>
<main class="workspace"><header class="page-header"><div class="heading-row"><button class="icon-btn hidden-side" data-action="sidebar" aria-label="展开侧栏">${icon('side')}</button>
<div class="identity"><span class="head-avatar">${icon('chat')}</span><div><h1 id="bot-title">XBot 主管</h1><span class="subtitle">你的长期协作伙伴</span></div></div><div class="header-actions"><span class="workspace-label">个人工作区</span>${btn('sun','切换深浅色','theme')}${btn('more','Bot 资料','info')}</div></div>
<div class="navigation-row"><nav class="tabs" role="tablist" aria-label="Bot 页面"></nav><div class="more-wrap"><button class="more-tab" id="more-tabs" aria-label="管理顶部标签" aria-expanded="false">${icon('more')}<span>更多</span></button><div class="popover tab-menu" id="tab-menu" hidden><p class="menu-title">顶部显示</p><div id="tab-options"></div><p class="menu-note">聊天固定在第一项，其余可按需收起</p></div></div></div></header>
${tabs.map(t=>`<section class="surface ${t.id==='chat'?'':'reading-surface'}" id="view-${t.id}" role="tabpanel" aria-label="${t.label}" ${t.id==='chat'?'':'hidden'}>${t.id==='chat'?views[t.id]:'<div class="reading-content">'+views[t.id]+'</div>'}</section>`).join('')}
</main></div><div class="toast" role="status" hidden></div><dialog id="info-dialog"><div class="dialog-heading"><h2 id="dialog-title"></h2><button class="icon-btn" id="close-dialog" aria-label="关闭">${icon('plus')}</button></div><div id="dialog-content"></div></dialog><input type="file" id="file-input" hidden>`;
// Static prototype: no API calls, all content is illustrative.
let active='chat';
const visible=new Set(tabs.map(t=>t.id));
const params=new URLSearchParams(location.search);
document.documentElement.dataset.theme=params.get('theme')==='dark'?'dark':'light';
function renderTabs(){
$('.tabs').innerHTML=tabs.filter(t=>visible.has(t.id)).map(t=>`<button class="tab" role="tab" id="tab-${t.id}" aria-controls="view-${t.id}" aria-selected="${active===t.id}" tabindex="${active===t.id?0:-1}" data-tab="${t.id}">${icon(t.icon)}${t.label}</button>`).join('');
tabs.forEach(t=>$('#view-'+t.id).setAttribute('aria-labelledby',visible.has(t.id)?'tab-'+t.id:'more-tabs'));
}
function closeMenu(){$('#tab-menu').hidden=true;$('#more-tabs').setAttribute('aria-expanded','false')}
function selectTab(id,focus=false){active=id;visible.add(id);tabs.forEach(t=>$('#view-'+t.id).hidden=t.id!==id);renderTabs();$('#tab-'+id).scrollIntoView({block:'nearest',inline:'nearest'});if(focus)$('#tab-'+id).focus();closeMenu()}
function renderOptions(){$('#tab-options').innerHTML=tabs.map(t=>`<button class="menu-option" data-option="${t.id}" ${t.id==='chat'?'disabled':''} aria-pressed="${visible.has(t.id)}">${icon(t.icon)}${t.label}${t.id==='chat'?'<small>固定</small>':visible.has(t.id)?icon('check'):''}</button>`).join('')}
selectTab(tabs.some(t=>t.id===params.get('tab'))?params.get('tab'):'chat');
// Composer utility buttons must not submit the message form.
$$('#composer button:not(.send)').forEach(b=>b.type='button');
document.addEventListener('click',e=>{
const withinMore=!!e.target.closest('.more-wrap');
const t=e.target.closest('[data-tab],[data-goto]');if(t)selectTab(t.dataset.tab||t.dataset.goto);
const o=e.target.closest('[data-option]');if(o&&o.dataset.option!=='chat'){const id=o.dataset.option;if(visible.has(id)){visible.delete(id);if(active===id)selectTab('chat')}else visible.add(id);renderTabs();renderOptions()}
if(!withinMore)closeMenu();
const a=e.target.closest('[data-action]');if(a&&actions[a.dataset.action])actions[a.dataset.action]();
});
$('.tabs').addEventListener('keydown',e=>{const list=$$('.tab');let i=list.indexOf(document.activeElement);if(e.key==='ArrowRight')i=(i+1)%list.length;else if(e.key==='ArrowLeft')i=(i-1+list.length)%list.length;else if(e.key==='Home')i=0;else if(e.key==='End')i=list.length-1;else return;e.preventDefault();selectTab(list[i].dataset.tab,true)});
$('#more-tabs').onclick=()=>{const open=$('#tab-menu').hidden;renderOptions();$('#tab-menu').hidden=!open;$('#more-tabs').setAttribute('aria-expanded',String(open))};
let toastTimer;
function toast(text){$('.toast').textContent=text;$('.toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('.toast').hidden=true,2800)}
function dialog(title,html){$('#dialog-title').textContent=title;$('#dialog-content').innerHTML=html;$('#info-dialog').showModal()}
$('#close-dialog').onclick=()=>$('#info-dialog').close();
$('#info-dialog').addEventListener('click',e=>{if(e.target===$('#info-dialog')){const r=e.target.getBoundingClientRect();if(e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom)e.target.close()}});
function chooseBot(name){$('#bot-title').textContent=name;$$('.bot-speaker').forEach(el=>el.textContent=name);$$('.bot').forEach(b=>b.classList.toggle('active',b.dataset.bot===name));$('.app').classList.remove('mobile-sidebar');selectTab('chat');toast('已切换到 '+name+' · 演示内容')}
$$('.bot').forEach(b=>b.onclick=()=>chooseBot(b.dataset.bot));
function openBotSearch(){dialog('查找 Bot','<input id="bot-filter" aria-label="查找 Bot" placeholder="输入 Bot 名称"><div id="bot-results"></div>');const draw=()=>{$('#bot-results').replaceChildren();bots.filter(b=>b[0].includes($('#bot-filter').value.trim())).forEach(b=>{const row=document.createElement('button');row.className='dialog-row';row.textContent=b[0];row.onclick=()=>{chooseBot(b[0]);$('#info-dialog').close()};$('#bot-results').append(row)})};$('#bot-filter').oninput=draw;draw();$('#bot-filter').focus()}
const actions={
theme:()=>{document.documentElement.dataset.theme=document.documentElement.dataset.theme==='dark'?'light':'dark'},
sidebar:()=>{if(matchMedia('(max-width:760px)').matches)$('.app').classList.toggle('mobile-sidebar');else $('.app').classList.toggle('side-collapsed')},
info:()=>dialog('Bot 资料','<p>名称：XBot 主管</p><p>职责：产品方向、任务协调和开发进度。</p><p>这是聊天页的交互原型，页面内容均为演示数据。</p>'),
profile:()=>dialog('个人工作区','<p>政东 · 本机已连接</p><p>顶部的太阳图标可以切换浅色与深色。</p>'),
new:()=>dialog('新建 Bot','<p>此原型重点展示聊天页布局。你可以从左侧选择现有 Bot，预览顶部身份区的变化。</p>'),
search:openBotSearch,
model:()=>{dialog('选择模型',['GPT-5','Claude Sonnet 4.5','DeepSeek V3.2'].map(n=>`<button class="dialog-row" data-model="${n}">${n}</button>`).join(''));$$('[data-model]').forEach(b=>b.onclick=()=>{$('.model-button').innerHTML=b.dataset.model+' '+icon('down');$('#info-dialog').close()})},
memory:()=>dialog('个人记忆 · memory.md','<p>政东正在开发 XBot，希望它是一个能长期协作、有记忆的个人助手。</p><p>偏好：中文交流，结论优先；界面简洁，UI 先看原型。</p><p>正式接入时，此入口打开已有的记忆文件。</p>'),
log:()=>dialog('11 月流水','<p>11 月 1 日 · 讨论顶部标签与聊天页面的布局方向。</p><p>这里用演示记录展示本月流水的阅读入口。</p>'),
attach:()=>$('#file-input').click(),voice:()=>toast('语音输入入口 · 原型演示')
};
document.addEventListener('keydown',e=>{if(e.key==='Escape'){closeMenu();$('.app').classList.remove('mobile-sidebar')}if((e.metaKey||e.ctrlKey)&&e.key==='k'){e.preventDefault();if(!$('#info-dialog').open)openBotSearch()}});
$('.switch').onclick=e=>{const b=e.currentTarget;b.setAttribute('aria-checked',String(b.getAttribute('aria-checked')!=='true'))};
$('#file-input').onchange=()=>{const file=$('#file-input').files[0];if(file)toast('已选择「'+file.name+'」· 原型不会上传文件')};
const draft=$('#draft');
draft.addEventListener('input',()=>$('.send').disabled=!draft.value.trim());
draft.addEventListener('keydown',e=>{if(e.key==='Enter'&&!e.shiftKey&&!e.isComposing){e.preventDefault();$('#composer').requestSubmit()}});
$('#composer').onsubmit=e=>{e.preventDefault();const text=draft.value.trim();if(!text)return;const bubble=document.createElement('div');bubble.className='message user';const body=document.createElement('div');body.className='user-text';body.textContent=text;bubble.append(body);$('.thread').append(bubble);draft.value='';$('.send').disabled=true;const reply=document.createElement('div');reply.innerHTML=assistant('<p>这是一条原型演示回复。切换标签时，聊天记录和输入草稿会保留，你可以继续浏览顶部的其他页面。</p>','刚刚');reply.querySelector('b').textContent=$('#bot-title').textContent;$('.thread').append(reply.firstElementChild);$('#messages').scrollTop=$('#messages').scrollHeight};
document.addEventListener('click',async e=>{const b=e.target.closest('.copy-message');if(!b)return;const text=b.closest('.assistant').querySelector('.assistant-body').innerText;try{await navigator.clipboard.writeText(text);toast('已复制回复')}catch{toast('请选中回复文字进行复制')}});
