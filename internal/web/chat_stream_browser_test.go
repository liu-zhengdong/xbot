package web

import (
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the shipped app. Only HTTP and the model stream are scripted.
func TestChatStreamBrowser(t *testing.T) {
	runChatStreamBrowser(t, chatStreamFixture, chatStreamDriver)
}

func runChatStreamBrowser(t *testing.T, fixture, driver string) {
	t.Helper()
	chrome, err := exec.LookPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	dir := t.TempDir()
	if evidence := os.Getenv("XBOT_CHAT_EVIDENCE"); evidence != "" {
		dir = filepath.Join(evidence, "fixture")
	}
	err = fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, strings.TrimPrefix(path, "static"))
		if d.IsDir() {
			return os.MkdirAll(dst, 0700)
		}
		raw, err := staticFS.ReadFile(path)
		if err != nil {
			return err
		}
		if path == "static/index.html" {
			page := strings.ReplaceAll(string(raw), `src="/`, `src="./`)
			page = strings.ReplaceAll(page, `href="/`, `href="./`)
			page = strings.Replace(page, "<head>", "<head><script>"+fixture+"</script>", 1)
			page = strings.Replace(page, "</body>", "<script>"+driver+"</script></body>", 1)
			raw = []byte(page)
		}
		return os.WriteFile(dst, raw, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--allow-file-access-from-files", "--user-data-dir="+filepath.Join(t.TempDir(), "chrome"), "--virtual-time-budget=15000", "--dump-dom", (&url.URL{Scheme: "file", Path: filepath.Join(dir, "index.html")}).String()).CombinedOutput()
	if err != nil {
		t.Fatalf("Chrome: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `data-test-result="passed"`) {
		result := string(out)
		if i := strings.Index(result, "data-test-result="); i >= 0 {
			result = result[i:min(i+1200, len(result))]
		} else if len(result) > 1500 {
			result = result[:1500]
		}
		t.Fatalf("chat stream browser: %s", result)
	}
}

const chatStreamFixture = `
localStorage.setItem('xbot-theme',new URLSearchParams(location.search).get('theme')||'light');
sessionStorage.removeItem('xbot-selected-bot');
window.testErrors=[];
window.addEventListener('error',e=>testErrors.push(e.message));
window.addEventListener('unhandledrejection',e=>testErrors.push(String(e.reason)));
const fixture={messages:{'2号小伙子':[],'另一个Bot':[]},streams:[],reads:0};
const bots=Object.keys(fixture.messages).map((name,i)=>({name,pinned:true,primary:i===0,avatar_seed:'glyph'}));
const json=v=>new Response(JSON.stringify(v),{headers:{'Content-Type':'application/json'}});
const message=(role,content)=>({seq:++fixture.seq,role,content,time:new Date().toISOString()});
fixture.seq=0;
window.fetch=async(path,opts={})=>{
 if(path==='/api/account/me')return json({account:{email:'zh***@example.com',owner:true,hosted:false,gateway:'xbot.babelgo.cn',official:true,gateway_ok:true}});
 if(path==='/api/session')return json({authed:true,owner:'owner',owner_name:'政东',version:'dev-test'});
 if(path==='/api/bots')return json({bots});
 if(path==='/api/groups')return json({groups:[],folders:[]});
 if(path==='/api/bots/status')return json({bots:[]});
 if(path==='/api/notes')return json({notes:[]});
 const match=String(path).match(/^\/api\/bots\/([^/]+)(\/transcript|\/messages)?$/);
 if(match){
  const bot=decodeURIComponent(match[1]);
  if(match[2]==='/transcript'){fixture.reads++;return json({messages:fixture.messages[bot]})}
  if(match[2]==='/messages'){
   fixture.messages[bot].push(message('user',JSON.parse(opts.body).text));
   let controller;const body=new ReadableStream({start(c){controller=c}});
   const stream={bot,controller};fixture.streams.push(stream);
   return new Response(body,{headers:{'Content-Type':'text/event-stream'}});
  }
  return json({name:bot,agents:[],links:[]});
 }
 return json({});
};
const emit=(s,event,payload)=>s.controller.enqueue(new TextEncoder().encode('event: '+event+'\ndata: '+JSON.stringify(payload)+'\n\n'));
// The bot speaks through send_message_to_user: say is that call landing in
// the stream, sayRows is how the transcript keeps it.
const say=(s,text)=>emit(s,'result',{name:'send_message_to_user',output:"sent to the owner's chat",text});
const sayRows=text=>{const id='say'+(++fixture.seq);return [{...message('assistant',''),tool_calls:[{id,function:{name:'send_message_to_user',arguments:JSON.stringify({text})}}]},{...message('tool',"sent to the owner's chat"),tool_call_id:id}]};
const finish=(s,content,error)=>{
 if(error)emit(s,'error',{error});
 else {
  if(content&&content!=='SILENT')fixture.messages[s.bot].push(...sayRows(content));
  else fixture.messages[s.bot].push(message('assistant',content));
  emit(s,'done',{text:content});
 }
 s.controller.close();
};
`

const chatStreamDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const text=()=>$('#thread').textContent;
const selected=name=>$('#title').textContent.includes(name)&&document.querySelector('[data-bot="'+name+'"].active')&&!$('#shell').hidden;
const send=async value=>{
 const n=fixture.streams.length;const box=$('#input');box.textContent=value;box.dispatchEvent(new Event('input'));$('#composer').requestSubmit();
 await waitFor(()=>fixture.streams.length>n&&$('#thread .live'),'send did not start');
 return fixture.streams.at(-1);
};
(async()=>{
 await waitFor(()=>selected('2号小伙子')&&!$('#thread .thread-skeleton'),'app did not boot');
 const s=await send('你现在是什么模型？');say(s,'我是助手。');
 await waitFor(()=>text().includes('我是助手。'),'message missing');
 finish(s,'我是助手。');
 await waitFor(()=>!$('#thread .live'),'stream did not settle');
 assert(selected('2号小伙子'),'selection cleared at completion');
 assert(text().includes('你现在是什么模型？'),'user message disappeared');
 assert(text().includes('我是助手。'),'sent message lost at completion');
 assert(!$('#thread .empty')&&!$('#thread .thread-unavailable'),'conversation replaced by empty/offline shell');
 const reads=fixture.reads;await waitFor(()=>fixture.reads>reads,'poll did not resume');
 assert(selected('2号小伙子')&&text().includes('我是助手。'),'poll erased the sent message');
 if(new URLSearchParams(location.search).has('shot')){document.body.dataset.testResult='passed';return}
 const normal=await send('再说一句');say(normal,'你好。');finish(normal,'你好。');
 await waitFor(()=>!$('#thread .live')&&text().includes('你好。'),'normal reply lost at completion');
 assert(selected('2号小伙子')&&text().includes('再说一句'),'normal completion left chat');
 const empty=await send('没有内容时呢');finish(empty,'','responses: empty response');
 await waitFor(()=>$('#thread .reply-error'),'empty response error missing');
 assert(selected('2号小伙子')&&text().includes('没有内容时呢'),'empty response error erased chat');
 assert($('#thread .reply-error').textContent.includes('这次没有收到回复'),'empty error is not readable');
 const late=await send('稍后再答');say(late,'旧的回复');
 await waitFor(()=>text().includes('旧的回复'),'late stream missing');
 $('[data-bot="另一个Bot"]').click();
 await waitFor(()=>selected('另一个Bot')&&!$('#thread .thread-skeleton'),'switch failed');
 finish(late,'迟到的回答');
 const other=await send('这里是另一个对话');finish(other,'另一个回答');
 await waitFor(()=>text().includes('另一个回答')&&!$('#thread .live'),'second bot did not settle');
 assert(selected('另一个Bot')&&!text().includes('迟到的回答'),'late completion overwrote new bot');
 $('[data-bot="2号小伙子"]').click();
 await waitFor(()=>selected('2号小伙子')&&text().includes('迟到的回答'),'history failed after switch back');
 // Intentional silence and tool-only steps must not become empty reply errors.
 fixture.messages['另一个Bot']=[message('assistant','SILENT'),{...message('assistant',''),tool_calls:[{id:'call_1',function:{name:'read_file',arguments:'{}'}}]}];
 $('[data-bot="另一个Bot"]').click();
 await waitFor(()=>selected('另一个Bot')&&!$('#thread .thread-skeleton'),'silent history did not load');
 assert(!text().includes('这次没有收到回复'),'silent/tool-only message became an error');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`
