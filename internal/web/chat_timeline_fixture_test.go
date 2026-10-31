package web

// Scripted browser scenarios retain transcript-shaped builders for their SSE
// and trace fixtures. Only this fixture adapter emits chat delivery DTOs; the
// shipped UI never projects transcript. Real HTTP tests exercise Go projection.
const chatTimelineFixture = `
function fixtureChatRows(messages) {
 const result=new Map(messages.filter(m=>m.role==='tool').map(m=>[m.tool_call_id,m.content]));
 const out=[];let run=[],id=0;
 const push=(m,text,kind,extra={})=>{
  const seq=m.seq||messages.indexOf(m)+1;id=Math.max(id+1,seq);
  out.push({id,transcript_seq:seq,delivery_key:kind+':'+id,kind,time:m.time,text,images:m.images,audios:m.audios,reply_to_seq:m.reply_to,meta:{branch:m.branch,parts:m.parts},from_device:m.from_device,relay:m.relay,...extra});
 };
 const close=()=>{
  const says=[],images=[],audios=[];let reply=0,media;
  for(const m of run){
   images.push(...(m.images||[]));audios.push(...(m.audios||[]));if(m.images?.length||m.audios?.length)media=m;if(m.reply_to)reply=m.reply_to;
   for(const c of m.tool_calls||[]){const r=result.get(c.id)||'';if(['send_message_to_user','notify_owner'].includes(c.function.name)&&(r.startsWith("sent to the owner's chat")||r.includes('notified the owner')||(m.delivered||[]).includes(c.id)))says.push({m,text:JSON.parse(c.function.arguments).text||''});}
  }
  if(!says.length&&media)says.push({m:media,text:''});
  if(says.length)says.forEach((s,i)=>{if(s.text&&s.text!=='SILENT'||i===says.length-1&&(images.length||audios.length))push(s.m,s.text==='SILENT'?'':s.text,'bot',{images:i===says.length-1?images:[],audios:i===says.length-1?audios:[],reply_to_seq:i===0?reply:0})});
  else for(const m of run)if(!(Date.parse(m.time)>=Date.parse('2026-10-27T03:00:00Z'))&&m.content!=='SILENT'&&(m.content||!(m.tool_calls||[]).length))push(m,m.content,'bot');
  run=[];
 };
 for(const m of messages){
  if(m.internal||m.kind==='fork'||m.kind==='fork_end')continue;
  if(m.role==='assistant'){run.push(m);continue}
  if(m.role!=='user')continue;
  close();const text=m.content||'',source=/^<untrusted_event\s+([^>]*)>\n([\s\S]*)\n<\/untrusted_event>$/.exec(text);
  if(source){const attrs=Object.fromEntries([...source[1].matchAll(/([\w-]+)="([^"]*)"/g)].map(a=>[a[1],a[2]]));if(['timer','shell'].includes(attrs.source)||attrs.source==='subagent'&&attrs.type==='fork_end')continue;push(m,text,'source')}
  else push(m,text,text.startsWith('<approval_card ')||text.startsWith('<coding_card ')?'card':'owner');
 }
 close();return out;
}
`
