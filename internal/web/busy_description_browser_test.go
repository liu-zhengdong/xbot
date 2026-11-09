package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// Hold the next model response after a real tool has finished.
type busyDescriptionProvider struct {
	waiting chan struct{}
	release chan struct{}
	called  bool
}

func (p *busyDescriptionProvider) Chat(ctx context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	if !p.called {
		p.called = true
		return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "status-step", Type: "function", Function: provider.FunctionCall{
				Name: "webstep", Arguments: `{"i":1,"description":"看账号管道聊天路由\nprivate command"}`,
			},
		}})}, nil
	}
	close(p.waiting)
	select {
	case <-p.release:
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")}, nil
	case <-ctx.Done():
		return provider.Message{}, ctx.Err()
	}
}

func TestBusyDescriptionBrowser(t *testing.T) {
	s, tok, bots := statusFixture(t, 1)
	p := &busyDescriptionProvider{waiting: make(chan struct{}), release: make(chan struct{})}
	k := &kernel.Kernel{Bot: bots[0], Provider: p, Memory: memory.NopStore{}, MaxSteps: 3,
		Links: []link.Link{link.Terminal{}, stepLink{}}, SkipBaseTools: true}
	s.kernels = map[string]*kernelEntry{bots[0].Name(): {Kernel: k, Gate: &sync.Mutex{}}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	done := make(chan error, 1)
	go func() { done <- k.Handle(ctx, inbox.TerminalEvent("u1", "check status", time.Now())) }()
	defer func() { cancel(); <-done }()
	select {
	case <-p.waiting:
	case <-ctx.Done():
		t.Fatal("next model call did not start")
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)
	statusBody := getAuth(t, ui.URL+"/api/bots/status", cookie)
	var status struct {
		Bots []kernel.BotStatus `json:"bots"`
	}
	if err := json.Unmarshal(statusBody, &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Bots) != 1 || status.Bots[0].State != kernel.StateRunning || status.Bots[0].Tool != "" || status.Bots[0].ToolSummary != "看账号管道聊天路由" {
		t.Fatalf("model wait lost description: %s", statusBody)
	}
	stepsBody := getAuth(t, ui.URL+"/api/bots/"+bots[0].Name()+"/steps?n=20", cookie)
	var steps struct {
		Steps []kernel.Step `json:"steps"`
	}
	if err := json.Unmarshal(stepsBody, &steps); err != nil || len(steps.Steps) != 1 || steps.Steps[0].State != kernel.StepDone {
		t.Fatalf("completed steps: %s, %v", stepsBody, err)
	}
	// Start a fresh page from the actual HTTP snapshots, without a tool SSE.
	fixture := todoBrowserFixture + fmt.Sprintf(`
Object.assign(todoStatus, (%s).bots[0], {bot:bots[0].name});
const busyBaseFetch=window.fetch;
window.fetch=async(path,opts={})=>String(path).endsWith('/steps?n=20')?json(%s):busyBaseFetch(path,opts);
`, statusBody, stepsBody)
	runTodoBrowser(t, fixture, todoLiveSetup+`
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&preview()?.textContent==='看账号管道聊天路由'&&!$('.thread-loading-cover'),'fresh page lost completed tool description');
 assert(fixture.streams.length===0,'progress required a live tool event');
 const row=preview(),text=row.querySelector('span'),reads=statusReads;
 await waitFor(()=>statusReads>=reads+2,'status polling stopped');
 assert(preview()===row&&row.querySelector('span')===text,'unchanged polling replaced progress nodes');
 row.click();await waitFor(()=>$('#stp li'),'steps did not open');
 assert($('#stp li').textContent.includes('step 1'),'completed step missing');
 $('#stp .x').click();
 assert(!/private command|webstep|shell/.test(preview().textContent),'raw tool details entered progress');
 if(new URLSearchParams(location.search).has('shot')){document.body.dataset.testResult='passed';return}
 todoStatus.state='idle';todoStatus.tool_summary='';todoStatus.todo='';todoStatus.last_message='查好了';
 await waitFor(()=>preview().textContent==='查好了','finished turn retained progress');
 todoStatus.state='running';todoStatus.last_message='';
 await waitFor(()=>preview().matches('.st.run')&&preview().textContent==='','new turn reused old progress');
 assert(testErrors.length===0,testErrors.join('; '));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
