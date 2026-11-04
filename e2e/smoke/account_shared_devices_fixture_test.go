//go:build smoke

package smoke

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/account"
	"xbot/internal/pipe"
	"xbot/internal/remote"
)

// Independent HTTP views follow UC-DEVICE-01/03 and the frozen HTTP contract.
// No new production device execution types are needed to compile this suite.
type accountSharedDevice struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Self         bool      `json:"self"`
	Online       bool      `json:"online"`
	OS           string    `json:"os"`
	Capabilities []string  `json:"capabilities"`
	LastSeen     time.Time `json:"last_seen"`
	RunningCount int       `json:"running_count"`
}

type accountSharedTask struct {
	ID          string     `json:"id"`
	Ref         string     `json:"ref"`
	DeviceID    string     `json:"device_id"`
	DeviceName  string     `json:"device_name"`
	Bot         string     `json:"bot"`
	ContextID   string     `json:"context_id"`
	Kind        string     `json:"kind"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Error       string     `json:"error"`
}

type accountSharedFixture struct {
	Source, Target *Server
	Model          *mcpRuntimeModel
	Device         accountSharedDevice
	TargetCalls    atomic.Int64
	PipeFrames     atomic.Int64
}

func accountSharedAPI(t *testing.T, s *Server, method, path string, body any) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("private device API transport failed: %s %s", method, path)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		t.Fatal("private device API response failed or exceeded 1MiB")
	}
	return res.StatusCode, raw
}

func accountSharedJSON(t *testing.T, s *Server, method, path string, body, dst any) {
	t.Helper()
	status, raw := accountSharedAPI(t, s, method, path, body)
	if status != http.StatusOK {
		t.Fatalf("%s %s: HTTP %d", method, path, status)
	}
	if dst != nil && json.Unmarshal(raw, dst) != nil {
		t.Fatalf("%s %s: malformed response", method, path)
	}
}

// The shared harness has no before-launch hook. This private constructor sets
// the local gateway before any account view can probe its health endpoint.
func accountSharedStart(t *testing.T, modelURL, gatewayURL string) *Server {
	t.Helper()
	base := t.TempDir()
	s := &Server{t: t, Root: filepath.Join(base, "data"), clockFile: filepath.Join(base, "clock")}
	if err := os.WriteFile(s.clockFile, []byte("0"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := remote.SetGateway(s.Root, gatewayURL); err != nil {
		t.Fatal(err)
	}
	s.env = envWith(os.Environ(), "HOME="+base, "XBOT_BASE_URL="+modelURL+"/v1",
		"XBOT_API_KEY=smoke", "XBOT_MODEL=smoke", "XBOT_AUTO_UPGRADE=0", "XBOT_FAKE_CLOCK_FILE="+s.clockFile)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	s.client = &http.Client{Jar: jar}
	t.Cleanup(s.Stop)
	s.launch()
	s.Login()
	return s
}

func newAccountSharedFixture(t *testing.T) *accountSharedFixture {
	t.Helper()
	cliSkillIsolation(t)
	for _, key := range []string{"XBOT_PUBLIC_URL", "XBOT_GATEWAY_TOKEN", "XBOT_CALL_ID"} {
		t.Setenv(key, "")
	}
	store, err := account.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Registration is verified immediately in the current real account service.
	acc := httptest.NewServer(account.New(store, nil, account.Config{}).Handler())
	t.Cleanup(acc.Close)
	f := &accountSharedFixture{Model: newMCPRuntimeModel(t)}
	hub := &pipe.Hub{Accounts: account.NewRemote(acc.URL), Log: log.New(io.Discard, "", 0)}
	handler := hub.Handler()
	hubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pipe/v1/ws" {
			f.PipeFrames.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	hub.Public = hubServer.URL
	t.Cleanup(hubServer.Close)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = io.WriteString(w, "ok")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(gateway.Close)
	t.Setenv("XBOT_ACCOUNT_API", acc.URL)
	t.Setenv("XBOT_PIPE_HUB", hubServer.URL)
	targetModel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.TargetCalls.Add(1)
		http.Error(w, "target model must not execute device work", http.StatusServiceUnavailable)
	}))
	t.Cleanup(targetModel.Close)
	f.Source = accountSharedStart(t, f.Model.server.URL, gateway.URL)
	f.Target = accountSharedStart(t, targetModel.URL, gateway.URL)
	// Stop children before their model, account, and pipe servers close.
	t.Cleanup(func() { f.Source.Stop(); f.Target.Stop() })
	credentials := map[string]string{"email": "device-owner@example.test", "password": "private smoke password 4821"}
	for i, s := range []*Server{f.Source, f.Target} {
		action := "register"
		if i == 1 {
			action = "login"
		}
		accountSharedJSON(t, s, http.MethodPost, "/api/account/"+action, credentials, nil)
		var claim struct {
			Bound    bool `json:"bound"`
			Reported bool `json:"reported"`
		}
		accountSharedJSON(t, s, http.MethodPost, "/api/account/claim", map[string]any{}, &claim)
		if !claim.Bound || !claim.Reported {
			t.Fatal("actual same-account install claim/report did not succeed")
		}
	}
	var targetList struct {
		Devices []accountSharedDevice `json:"devices"`
	}
	accountSharedJSON(t, f.Target, http.MethodGet, "/api/account/devices", nil, &targetList)
	for _, d := range targetList.Devices {
		if d.Self {
			f.Device = d
		}
	}
	if f.Device.ID == "" {
		t.Fatal("target account directory has no actual self identity")
	}
	WaitFor(t, 8*time.Second, "both real serve standby WebSockets", func() (bool, string) {
		return f.PipeFrames.Load() >= 2, fmt.Sprintf("connections=%d", f.PipeFrames.Load())
	})
	var sourceList struct {
		Devices []accountSharedDevice `json:"devices"`
	}
	accountSharedJSON(t, f.Source, http.MethodGet, "/api/account/devices", nil, &sourceList)
	found := false
	for _, d := range sourceList.Devices {
		if d.ID == f.Device.ID && !d.Self && d.Online {
			found = true
		}
	}
	if !found {
		t.Fatal("source cannot discover the online same-account target without pairing")
	}
	f.Source.CreateBot("DeviceAlpha", "Execute selected account device work from this source Bot.")
	f.Source.CreateBot("DeviceBeta", "Execute selected account device work from another source Bot.")
	return f
}

// device_id comes from the original owner message's prepared Shell context.
// Deliberately omit --device so this tests message selection propagation.
func accountSharedCommand(command string) string {
	args, _ := json.Marshal(map[string]string{"command": command, "description": "Write an acceptance marker on the selected account device"})
	return `"$XBOT_BIN" device exec --self --json --args ` + shellQuote(string(args))
}

func accountSharedSend(t *testing.T, s *Server, bot, cue, deviceID string) {
	t.Helper()
	status, raw := accountSharedAPI(t, s, http.MethodPost, "/api/bots/"+url.PathEscape(bot)+"/messages",
		map[string]string{"text": cue, "device_id": deviceID})
	if status != http.StatusOK {
		t.Fatalf("selected-device owner message: HTTP %d", status)
	}
	scan := bufio.NewScanner(bytes.NewReader(raw))
	scan.Buffer(make([]byte, 4096), 1<<20)
	var event string
	done := false
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			if event == "error" || event == "approval" {
				t.Fatalf("selected-device message emitted %s", event)
			}
			if event == "done" {
				done = true
			}
		}
	}
	if scan.Err() != nil || !done {
		t.Fatal("selected-device message has no complete SSE stream")
	}
}

func (f *accountSharedFixture) run(t *testing.T, bot, cue, command string) string {
	t.Helper()
	result := f.runCLI(t, bot, cue, accountSharedCommand(command))
	var receipt struct {
		Task accountSharedTask `json:"task"`
	}
	if json.Unmarshal([]byte(commandOutput(result)), &receipt) != nil || receipt.Task.Ref == "" || receipt.Task.DeviceID != f.Device.ID || receipt.Task.Bot != bot {
		t.Fatal("source runtime CLI lost actual task reference, device_id, or Bot")
	}
	return result
}

func (f *accountSharedFixture) terminalReceipt(t *testing.T, bot, cue string, task accountSharedTask) {
	t.Helper()
	result := f.runCLI(t, bot, cue, `"$XBOT_BIN" device task `+shellQuote(task.Ref)+` --self --json`)
	var receipt struct {
		Task   accountSharedTask `json:"task"`
		Output string            `json:"output"`
	}
	if json.Unmarshal([]byte(commandOutput(result)), &receipt) != nil || receipt.Task.Ref != task.Ref || receipt.Task.DeviceID != f.Device.ID || receipt.Task.Status != task.Status {
		t.Fatal("source final CLI receipt lost actual terminal task/device identity")
	}
}

func (f *accountSharedFixture) runCLI(t *testing.T, bot, cue, command string) string {
	t.Helper()
	f.Model.add(cue,
		mcpRuntimeTool("sh", map[string]string{"description": "Use the original source runtime device CLI", "command": command}),
		func(results []string) (string, any) {
			return "send_message_to_user", map[string]string{"text": cue + " RECEIPT " + commandOutput(results[0])}
		},
	)
	accountSharedSend(t, f.Source, bot, cue, f.Device.ID)
	var result string
	WaitFor(t, 8*time.Second, "real source CLI receipt and original delivery", func() (bool, string) {
		results := f.Model.results(cue)
		if len(results) == 2 {
			result = results[0]
		}
		return result != "" && len(results) == 2 && results[1] != "", "source tool results not yet observed"
	})
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("device CLI failed from actual source Shell: %.512s", result)
	}
	WaitFor(t, 8*time.Second, "source final receipt with actual device identity", func() (bool, string) {
		for _, row := range f.Source.ChatRows(bot) {
			if row.Role == "assistant" && strings.Contains(row.Content, cue+" RECEIPT ") && strings.Contains(row.Content, f.Device.ID) {
				return true, ""
			}
		}
		return false, "original Bot delivery lacks actual device_id"
	})
	WaitFor(t, 8*time.Second, "source persisted terminal transcript row", func() (bool, string) {
		for _, row := range f.Source.Transcript(bot) {
			if row.Role == "assistant" && len(row.ToolCalls) == 0 && row.Content == cue+"_FINISHED" {
				return true, ""
			}
		}
		return false, "source final model response has not persisted"
	})
	return result
}

func (f *accountSharedFixture) tasks(t *testing.T, bot string) []accountSharedTask {
	t.Helper()
	var list struct {
		Tasks []accountSharedTask `json:"tasks"`
	}
	accountSharedJSON(t, f.Source, http.MethodGet, "/api/account/devices/"+url.PathEscape(f.Device.ID)+"/tasks?bot="+url.QueryEscape(bot), nil, &list)
	return list.Tasks
}

func (f *accountSharedFixture) waitTask(t *testing.T, bot, status string) accountSharedTask {
	t.Helper()
	var task accountSharedTask
	WaitFor(t, 12*time.Second, "actual device task "+status, func() (bool, string) {
		list := f.tasks(t, bot)
		if len(list) != 1 {
			return false, fmt.Sprintf("tasks=%d", len(list))
		}
		task = list[0]
		return task.Status == status, "status=" + task.Status
	})
	if task.ID == "" || task.Ref == "" || task.DeviceID != f.Device.ID || task.DeviceName == "" || task.Bot != bot || task.ContextID == "" || task.Kind == "" || task.StartedAt.IsZero() {
		t.Fatal("device task lost execution identity, original Bot, or actual start time")
	}
	if status != "running" && (task.FinishedAt == nil || task.FinishedAt.IsZero()) {
		t.Fatal("terminal task has no actual finished_at")
	}
	var source struct {
		Tasks []accountSharedTask `json:"tasks"`
	}
	accountSharedJSON(t, f.Source, http.MethodGet, "/api/bots/"+url.PathEscape(bot)+"/device-tasks", nil, &source)
	found := false
	for _, row := range source.Tasks {
		if row.Ref == task.Ref && row.DeviceID == f.Device.ID && row.Bot == bot {
			found = true
		}
	}
	if !found {
		t.Fatal("original Bot task API lost device_id or task reference")
	}
	return task
}

func accountSharedFiles(t *testing.T, root, basename string) []string {
	t.Helper()
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == basename {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func (f *accountSharedFixture) marker(t *testing.T, name, content string) string {
	t.Helper()
	var path string
	WaitFor(t, 8*time.Second, "marker on target disk", func() (bool, string) {
		files := accountSharedFiles(t, f.Target.Root, name)
		if len(files) != 1 {
			return false, fmt.Sprintf("target matches=%d", len(files))
		}
		path = files[0]
		raw, err := os.ReadFile(path)
		return err == nil && string(raw) == content, "target content does not match"
	})
	if len(accountSharedFiles(t, f.Source.Root, name)) != 0 {
		t.Fatal("remote marker also appeared on source disk")
	}
	return path
}

func (f *accountSharedFixture) noDelegate(t *testing.T) {
	t.Helper()
	if f.TargetCalls.Load() != 0 {
		t.Fatalf("target device execution invoked a target model: requests=%d", f.TargetCalls.Load())
	}
	if f.PipeFrames.Load() <= 2 {
		t.Fatal("execution never opened a real pipe connection beyond the two standby sockets")
	}
	for _, s := range []*Server{f.Source, f.Target} {
		cfg, err := remote.Load(s.Root)
		if err != nil || len(cfg.Devices) != 0 {
			t.Fatal("account execution unexpectedly required old remote pairing")
		}
	}
}
