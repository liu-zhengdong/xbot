package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

// The shipped page and HTTP/kernel assembly, real MediaRecorder and decoder.
// Only the microphone device and chat model are fake; no browser media API or
// application request is replaced. A fresh profile keeps service workers out.
func TestVoiceRealBrowser(t *testing.T) {
	chrome := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(chrome); err != nil {
		t.Skip("Chrome unavailable")
	}
	root, upstream, cookie, _ := pasteTestServer(t, false)
	defer upstream.Close()
	id := uploadVoice(t, upstream.URL, cookie, voiceWAV())
	b, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{provider.RoleUser, provider.RoleAssistant} {
		if err = b.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: role, Content: provider.TextContent("语音播放验证。")}, Audios: []string{id}}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Replace(string(raw), "</body>", "<script>"+voiceRealBrowserDriver+"</script></body>", 1)
	ui := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.SetCookie(w, cookie)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, page)
			return
		}
		upstream.Config.Handler.ServeHTTP(w, r)
	}))
	defer ui.Close()
	out, err := exec.Command("node", filepath.Join("testdata", "voice-real.cjs"), chrome, ui.URL, filepath.Join(t.TempDir(), "chrome")).CombinedOutput()
	if err != nil {
		t.Fatalf("real voice browser: %v %s", err, out)
	}
	if !strings.Contains(string(out), "passed") {
		t.Fatalf("real voice browser: %s", out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

const voiceRealBrowserDriver = `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const waitFor=async(fn,msg)=>{for(let i=0;i<1800;i++){if(fn())return;await sleep(10)}throw Error(msg)};
(async()=>{
 await waitFor(()=>$('#thread .msg.user audio')&&$('#thread .msg.bot audio'),'boot');
 const audio=$('#thread .msg.user audio'),botAudio=$('#thread .msg.bot audio');
 assert(audio.paused&&botAudio.paused,'autoplay');
 $('#thread .msg.user .v-play').click();await waitFor(()=>!audio.paused&&audio.readyState>=2,'user playback');
 $('#thread .msg.bot .v-play').click();await waitFor(()=>!botAudio.paused&&botAudio.readyState>=2,'bot playback');
 assert(audio.paused,'both playing');botAudio.pause();
 $('#input').textContent='这份草稿不随录音发送';$('#input').dispatchEvent(new Event('input',{bubbles:true}));
 $('#mic').click();await sleep(1600);assert($('#composer').classList.contains('recording'),'mic start');
 $('#rec-send').click();
 await waitFor(()=>[...document.querySelectorAll('#thread .msg.user audio')].some(a=>a.src.endsWith('.webm')),'recording did not reach transcript');
 assert($('#input').textContent==='这份草稿不随录音发送','draft changed');
 const recorded=[...document.querySelectorAll('#thread .msg.user audio')].find(a=>a.src.endsWith('.webm'));
 recorded.closest('.voice').querySelector('.v-play').click();await waitFor(()=>!recorded.paused&&recorded.readyState>=2,'recorded playback');recorded.pause();
 const samples=[];for(let i=0;i<25;i++){const at=performance.now();const r=await fetch(recorded.src,{headers:{Range:'bytes=0-511'}});assert(r.status===206,'range');await r.arrayBuffer();samples.push(performance.now()-at)}
 samples.sort((a,b)=>a-b);document.body.dataset.voicePerformance=JSON.stringify({rangeP95ms:samples[23],samples:25});
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.stack});
`
