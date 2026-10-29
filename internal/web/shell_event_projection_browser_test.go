package web

import "testing"

func TestShellCompletionStaysOutOfChatBrowser(t *testing.T) {
	runTodoBrowser(t, `
localStorage.removeItem('xbot-panel-open');localStorage.removeItem('xbot.side');
bots.forEach(b=>b.pinned=false);
const event=(source,text)=>message('user','<untrusted_event source="'+source+'" type="shell_job">\n'+text+'\n</untrusted_event>');
fixture.messages[bots[0].name].push(message('user','请检查结果。'),
 event('shell','工具 bash（sh123456）：已结束，输出 bash-result'),
 event('shell','工具 compact（sh234567）：已结束，输出 compact-result'),
 event('webhook','外部通知仍可见'),...sayRows('检查完成。'));
`, `
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
(async()=>{
 for(let i=0;i<1000&&!document.querySelector('#thread')?.textContent.includes('检查完成。');i++)await sleep(10);
 const chat=document.querySelector('#thread').textContent;
 for(const text of ['请检查结果。','检查完成。','Webhook：外部通知仍可见'])
  if(!chat.includes(text))throw Error('chat missing '+text+': '+chat);
 for(const text of ['后台命令','bash-result','compact-result'])
  if(chat.includes(text))throw Error('chat shows shell completion: '+text);
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
