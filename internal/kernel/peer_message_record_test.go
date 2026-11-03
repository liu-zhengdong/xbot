package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/exec"
	"xbot/internal/gateway"
	"xbot/internal/inbox"
	"xbot/internal/peer"
	"xbot/internal/person"
	"xbot/internal/provider"
	"xbot/internal/remote"
	"xbot/internal/tools"
)

// This fixture uses a real loopback gateway and public invite/join/Receive. No
// production transport injection or default-client replacement is involved.
type peerMessageRuntimeGateway struct {
	mu       sync.Mutex
	mode     string
	attempts int
	after    func() error
	hookErr  error
	root     string
	url      string
	receiver *bot.Bot
	client   *gateway.Client
	self     string
}

func peerMessageRuntimePair(t *testing.T, k *Kernel) *peerMessageRuntimeGateway {
	t.Helper()
	f := &peerMessageRuntimeGateway{mode: "setup", root: t.TempDir()}
	now := time.Now().UTC()
	k.Now = func() time.Time { return now }
	base := (&gateway.Server{Dir: t.TempDir(), Token: "fixture-join", Now: k.Now}).Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		mode, after := f.mode, f.after
		if r.URL.Path == "/v1/send" && mode != "setup" {
			f.attempts++
		}
		f.mu.Unlock()
		if r.URL.Path != "/v1/send" || mode == "setup" {
			base.ServeHTTP(w, r)
			return
		}
		if mode == "503" || mode == "400" {
			status, _ := strconv.Atoi(mode)
			http.Error(w, "fixture rejected", status)
			return
		}
		recorder := httptest.NewRecorder()
		base.ServeHTTP(recorder, r)
		if after != nil && recorder.Code >= 200 && recorder.Code < 300 {
			if err := after(); err != nil {
				f.mu.Lock()
				f.hookErr = err
				f.mu.Unlock()
			}
		}
		for name, values := range recorder.Header() {
			w.Header()[name] = values
		}
		if mode == "lost_response" {
			w.Header().Set("Content-Length", strconv.Itoa(recorder.Body.Len()+50))
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	t.Cleanup(server.Close)
	f.url = server.URL
	t.Setenv("XBOT_GATEWAY_TOKEN", "fixture-join")
	for _, root := range []string{k.Bot.Root, f.root} {
		owner, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:peer-runtime"}, "fixture owner")
		if err != nil {
			t.Fatal(err)
		}
		if root == k.Bot.Root {
			if err := k.Bot.SetOwners([]string{owner.ID}); err != nil {
				t.Fatal(err)
			}
		} else {
			f.receiver = botControlMake(t, root, "B", owner.ID)
		}
		if err := remote.SetGateway(root, server.URL); err != nil {
			t.Fatal(err)
		}
	}
	priv, self, _, err := remote.Identity(k.Bot.Root)
	if err != nil {
		t.Fatal(err)
	}
	sender := &gateway.Client{Base: server.URL, Key: priv}
	if err := sender.Register(context.Background(), "fixture-join"); err != nil {
		t.Fatal(err)
	}
	code, err := remote.CreateInvite(k.Bot.Root, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Join(context.Background(), f.root, code, "Sender", "Laptop", "", now); err != nil {
		t.Fatal(err)
	}
	envs, err := sender.Poll(context.Background(), 0)
	if err != nil || len(envs) != 1 {
		t.Fatalf("public pairing poll: count=%d err=%v", len(envs), err)
	}
	ack, _, err := remote.Receive(k.Bot.Root, self, envs, now)
	if err != nil || len(ack) != 1 {
		t.Fatalf("public pairing Receive: ack=%d err=%v", len(ack), err)
	}
	if err := sender.Ack(context.Background(), ack); err != nil {
		t.Fatal(err)
	}
	key, receiverID, _, err := remote.Identity(f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.self, f.client = receiverID, &gateway.Client{Base: server.URL, Key: key}
	f.mode = "accepted"
	return f
}

func peerMessageRuntimeFact(t *testing.T, b *bot.Bot, seq int) (bot.Entry, peer.RemoteOutgoingFact) {
	t.Helper()
	entries, err := b.Slice(seq, seq)
	if err != nil || len(entries) != 1 {
		t.Fatalf("source fact sequence %d: count=%d err=%v", seq, len(entries), err)
	}
	e := entries[0]
	if e.Origin != bot.OriginInternal || e.Message.Role != provider.RoleUser || e.EventID != "" || e.Blob != "" || len(e.Message.Calls()) != 0 || e.Message.CallID() != "" {
		t.Fatal("outgoing record is not an ordinary full internal user note")
	}
	const prefix, suffix = "<peer_message_outgoing>\n", "\n</peer_message_outgoing>"
	text := e.Message.Text()
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, suffix) {
		t.Fatal("outgoing fact did not use the exact dedicated format")
	}
	var fact peer.RemoteOutgoingFact
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(text, prefix), suffix)), &fact); err != nil {
		t.Fatal(err)
	}
	return e, fact
}

func TestPeerMessageRuntimeRemoteFullFactActualOwnerAndReceive(t *testing.T) {
	k, endpoint := peerMessageRuntimeFixture(t)
	f := peerMessageRuntimePair(t, k)
	owner := k.ensureTurn()
	owner.turnChain, owner.turnHops = "ch_frozen", 1
	// Preserve the original 64 KiB encrypted-envelope cap while exercising an
	// exact 8 KiB body whose escaped JSON still exceeds the Shell receipt budget.
	body := "HEAD_SOURCE_SECRET" + strings.Repeat("\x01", 5200) + strings.Repeat("a", peer.MaxText-len("HEAD_SOURCE_SECRET")-5200-len("TAIL")) + "TAIL"
	if len(body) != peer.MaxText {
		t.Fatal("invalid 8KiB fixture")
	}
	k.Redact = func(s string) string { return strings.ReplaceAll(s, "SOURCE_SECRET", "[source]") }
	k.secretMu.Lock()
	k.secrets = map[string]map[string]string{"fixture": {"ephemeral": "TAIL"}}
	k.secretMu.Unlock()
	file := filepath.Join(k.WorkDir, "message.txt")
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	var reply cliops.PeerMessageReply
	var callID string
	runRoutineSource(t, k, nil, `{"command":"peer-message B@Laptop --self --file message.txt"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		frozen, _ := ctx.Value(turnKey{}).(*turn)
		execution, _ := ctx.Value(callTurnKey{}).(*turn)
		if frozen == nil || execution != owner || frozen == execution {
			t.Error("fixture lost actual prepared/execution separation")
		}
		owner.mu.Lock()
		owner.branch, owner.turnChain, owner.turnHops = "bg_original", "ch_changed", 3
		owner.mu.Unlock()
		k.turnMu.Lock()
		k.turn = &turn{branch: "latest", turnChain: "ch_latest"}
		k.turnMu.Unlock()
		decoded := cliops.PeerMessage{Bot: " \tB@Laptop\n", Text: body}
		if err := os.WriteFile(file, []byte("changed after request decode"), 0600); err != nil {
			t.Error(err)
		}
		callID = tools.CallID(ctx)
		var err error
		reply, err = cliops.CallPeerMessage(ctx, endpoint, k.Bot.Root, callID, decoded)
		if err != nil || reply.Posted || reply.EventID != "" || reply.Gateway != "accepted" || reply.EnvelopeID == "" || reply.Queued || reply.Recorded == nil || !*reply.Recorded || reply.RecordSeq < 1 {
			t.Errorf("actual remote/source facts: %+v err=%v", reply, err)
		}
		peerMessageRuntimeReceipt(t, reply)
		return "completed", nil
	})
	e, fact := peerMessageRuntimeFact(t, k.Bot, reply.RecordSeq)
	wantBody := strings.ReplaceAll(strings.ReplaceAll(body, "SOURCE_SECRET", "[source]"), "TAIL", "[secret]")
	if e.Branch != "bg_original" || fact.Text != wantBody || fact.Bot != "B@Laptop" || fact.CallID != callID || fact.EnvelopeID != reply.EnvelopeID || fact.Gateway != "accepted" || fact.Error != "" || !e.Time.Equal(k.now()) {
		t.Fatal("full frozen request, actual owner branch, privacy, or original result fact lost")
	}
	rows, err := peer.RemoteConversation(k.Bot, "B@Laptop")
	if err != nil || len(rows) != 1 || rows[0].Text != wantBody || rows[0].Status != "sent" || rows[0].ID != fmt.Sprintf("peer_message_%d", reply.RecordSeq) {
		t.Fatalf("real outgoing consumer: rows=%d err=%v", len(rows), err)
	}
	// The owner can type the exact same tag and JSON; only the actual ordinary
	// internal record is an outgoing fact.
	if err := k.Bot.Append(bot.Entry{Time: k.now(), Origin: bot.OriginOwner, Message: e.Message}); err != nil {
		t.Fatal(err)
	}
	if rows, err := peer.RemoteConversation(k.Bot, "B@Laptop"); err != nil || len(rows) != 1 {
		t.Fatalf("owner fact injection accepted: rows=%d err=%v", len(rows), err)
	}
	// Gateway delivery is real signed/encrypted traffic, independent of source
	// display redaction. Receive owns its own event timestamp and seen dedup.
	envs, err := f.client.Poll(context.Background(), 0)
	if err != nil || len(envs) != 1 || envs[0].ID != reply.EnvelopeID || envs[0].Kind != "box" || strings.Contains(string(envs[0].Body), "HEAD") {
		t.Fatalf("actual encrypted gateway envelope: count=%d err=%v", len(envs), err)
	}
	receivedAt := k.now().Add(time.Second)
	for i := 0; i < 2; i++ {
		ack, _, err := remote.Receive(f.root, f.self, envs, receivedAt)
		if err != nil || len(ack) != 1 {
			t.Fatalf("Receive/duplicate attempt %d: ack=%d err=%v", i, len(ack), err)
		}
	}
	box, err := inbox.Open(f.receiver.InboxPath())
	if err != nil || len(box.Pending()) != 1 {
		t.Fatalf("actual Receive event missing or duplicated: %v", err)
	}
	ev := box.Pending()[0]
	if ev.ID != "remote_"+envs[0].From+"_"+reply.EnvelopeID || ev.Chain != "ch_frozen" || ev.Hops != 2 || ev.Content != body || ev.Sender != "ada@Sender" || !ev.Time.Equal(receivedAt) || !ev.Untrusted || ev.SenderKind != inbox.KindBot {
		t.Fatal("actual Receive metadata/body lost frozen sender tuple")
	}
}

func TestPeerMessageRuntimeRemoteResultStatesAndQueueFacts(t *testing.T) {
	for _, tc := range []struct {
		name, mode, gateway, status   string
		queued, queueFault, wantError bool
		code                          int
	}{
		{"accepted", "accepted", "accepted", "sent", false, false, false, 0},
		{"queued", "503", "http_error", "queued", true, false, false, 503},
		{"permanent", "400", "http_error", "failed", false, false, true, 400},
		{"accepted_response_lost_queued", "lost_response", "unknown", "queued", true, false, false, 0},
		{"accepted_response_lost_queue_failure", "lost_response", "unknown", "unknown", false, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, endpoint := peerMessageRuntimeFixture(t)
			f := peerMessageRuntimePair(t, k)
			f.mu.Lock()
			f.mode = tc.mode
			f.mu.Unlock()
			if tc.queueFault {
				if err := os.WriteFile(filepath.Join(k.Bot.Root, "remote", "outbox"), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var reply cliops.PeerMessageReply
			runRoutineSource(t, k, nil, `{"command":"peer-message B@Laptop --self --text body"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				var err error
				reply, err = cliops.CallPeerMessage(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.PeerMessage{Bot: "B@Laptop", Text: "body"})
				if (err != nil) != tc.wantError || reply.Gateway != tc.gateway || reply.GatewayStatus != tc.code || reply.Queued != tc.queued || reply.EnvelopeID == "" || reply.Posted || reply.EventID != "" || reply.Recorded == nil || !*reply.Recorded || reply.RecordSeq < 1 {
					t.Errorf("remote partial facts: %+v err=%v", reply, err)
				}
				peerMessageRuntimeReceipt(t, reply)
				return "completed", nil
			})
			_, fact := peerMessageRuntimeFact(t, k.Bot, reply.RecordSeq)
			if fact.Text != "body" || fact.Gateway != tc.gateway || fact.Queued != tc.queued || fact.EnvelopeID != reply.EnvelopeID || (fact.Error != "") != tc.wantError {
				t.Fatal("ordinary source fact differs from actual original send result")
			}
			rows, err := peer.RemoteConversation(k.Bot, "B@Laptop")
			if err != nil || len(rows) != 1 || rows[0].Status != tc.status {
				t.Fatalf("consumer status: rows=%+v err=%v", rows, err)
			}
			if tc.queued {
				raw, err := os.ReadFile(filepath.Join(k.Bot.Root, "remote", "outbox", reply.EnvelopeID+".json"))
				var item struct {
					Env gateway.Envelope `json:"env"`
				}
				if err != nil || json.Unmarshal(raw, &item) != nil || item.Env.ID != reply.EnvelopeID {
					t.Fatalf("queued fact has no original durable envelope: %v", err)
				}
			}
			if tc.mode == "lost_response" {
				envs, err := f.client.Poll(context.Background(), 0)
				if err != nil || len(envs) != 1 || envs[0].ID != reply.EnvelopeID {
					t.Fatalf("unknown incorrectly claimed gateway had no envelope: count=%d err=%v", len(envs), err)
				}
			}
			f.mu.Lock()
			attempts := f.attempts
			f.mu.Unlock()
			if attempts != 1 {
				t.Fatalf("runtime resent original attempt: %d", attempts)
			}
		})
	}
}

func TestPeerMessageRuntimeRemoteRecordFailureRetainsAcceptedAndQueued(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprintf("queued_%t", queued), func(t *testing.T) {
			k, endpoint := peerMessageRuntimeFixture(t)
			f := peerMessageRuntimePair(t, k)
			if _, err := k.Bot.Count(); err != nil {
				t.Fatal(err)
			}
			lock := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "transcript.lock")
			obstruct := func() error {
				if err := os.Rename(lock, lock+".saved"); err != nil {
					return err
				}
				return os.Mkdir(lock, 0700)
			}
			if queued {
				// Enqueue still succeeds; only the later source transcript is bad.
				if err := obstruct(); err != nil {
					t.Fatal(err)
				}
				f.mode = "503"
			} else {
				f.after = obstruct // run only after the real gateway accepted
			}
			runRoutineSource(t, k, nil, `{"command":"peer-message B@Laptop --self --text body"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				reply, err := cliops.CallPeerMessage(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.PeerMessage{Bot: "B@Laptop", Text: "body"})
				wantGateway := "accepted"
				if queued {
					wantGateway = "http_error"
				}
				if err == nil || reply.Stage != "record" || reply.Recorded == nil || *reply.Recorded || reply.RecordSeq != 0 || reply.Gateway != wantGateway || reply.Queued != queued || reply.EnvelopeID == "" {
					t.Errorf("record failure erased actual send facts: %+v err=%v", reply, err)
				}
				peerMessageRuntimeReceipt(t, reply)
				if queued {
					if _, err := os.Stat(filepath.Join(k.Bot.Root, "remote", "outbox", reply.EnvelopeID+".json")); err != nil {
						t.Error(err)
					}
				} else {
					envs, err := f.client.Poll(ctx, 0)
					if err != nil || len(envs) != 1 || envs[0].ID != reply.EnvelopeID {
						t.Errorf("actual gateway acceptance lost: count=%d err=%v", len(envs), err)
					}
				}
				return "completed", nil
			})
			if err := os.Remove(lock); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(lock+".saved", lock); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			attempts, hookErr := f.attempts, f.hookErr
			f.mu.Unlock()
			if hookErr != nil || attempts != 1 {
				t.Fatalf("record fault ordering/retry: attempts=%d err=%v", attempts, hookErr)
			}
			entries, err := k.Bot.Entries()
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed source append was claimed successful: count=%d err=%v", len(entries), err)
			}
		})
	}
}

func TestPeerMessageRuntimeRemotePolicyAndEntryBound(t *testing.T) {
	k, endpoint := peerMessageRuntimeFixture(t)
	var first cliops.PeerMessageReply
	runRoutineSource(t, k, nil, `{"command":"peer-message B@missing --self --text body"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		var err error
		first, err = cliops.CallPeerMessage(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.PeerMessage{Bot: "B@missing", Text: "full rejected request"})
		if err == nil || first.Gateway != "not_attempted" || first.EnvelopeID != "" || first.Recorded == nil || !*first.Recorded {
			t.Errorf("not-attempted source fact lost: %+v err=%v", first, err)
		}
		// The HTTP request fits 1MiB, while encoding a full escaped ordinary
		// record would exceed it. Keep the original domain error and fail record.
		text := strings.Repeat("\x01", 160000)
		reply, err := cliops.CallPeerMessage(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.PeerMessage{Bot: "B@missing", Text: text})
		if err == nil || reply.Gateway != "not_attempted" || reply.Stage != "record" || reply.Recorded == nil || *reply.Recorded || reply.RecordSeq != 0 || !strings.Contains(reply.Error, "1 MiB") {
			t.Errorf("oversized ordinary entry was truncated/accepted: %+v err=%v", reply, err)
		}
		return "completed", nil
	})
	_, fact := peerMessageRuntimeFact(t, k.Bot, first.RecordSeq)
	if fact.Text != "full rejected request" || fact.Error == "" {
		t.Fatal("policy failure note lost full frozen request/original error")
	}
	entries, err := k.Bot.Entries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("entry bound appended a partial/second note: count=%d err=%v", len(entries), err)
	}
}

func TestPeerMessageRuntimeRemoteActualChildOwner(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		toolCall("child-source", "sh", `{"command":"peer-message B@Laptop --self --text child"}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("child complete")},
	}}
	k, b, box := dispatchKernel(t, p)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	f := peerMessageRuntimePair(t, k)
	_ = f
	endpoint := operationEndpoint(k)
	_ = k.ensureExec()
	local, ok := k.execSnapshot().(*exec.Local)
	if !ok {
		t.Fatal("executor is not Local")
	}
	called := make(chan error, 1)
	originalTools := local.ToolsFor
	local.ToolsFor = func(req exec.StartRequest) []tools.Tool {
		list := originalTools(req)
		for i, tool := range list {
			if tool.Name() == "sh" {
				list[i] = routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
					prepared, _ := ctx.Value(turnKey{}).(*turn)
					execution, _ := ctx.Value(callTurnKey{}).(*turn)
					source, _ := ctx.Value(routineCallSourceKey{}).(routineCallSource)
					if prepared == nil || execution == nil || execution == prepared || !source.child || source.owner != nil {
						called <- errors.New("actual child prepared identity unavailable")
						return "", errors.New("actual child identity unavailable")
					}
					execution.mu.Lock()
					execution.branch = "bg_actual_child"
					execution.mu.Unlock()
					k.turnMu.Lock()
					k.turn = &turn{branch: "latest_parent"}
					k.turnMu.Unlock()
					reply, err := cliops.CallPeerMessage(ctx, endpoint, b.Root, tools.CallID(ctx), cliops.PeerMessage{Bot: "B@Laptop", Text: "child complete body"})
					if err == nil && (reply.Recorded == nil || !*reply.Recorded) {
						err = errors.New("child outgoing fact was not recorded")
					}
					if err == nil {
						e, fact := peerMessageRuntimeFact(t, b, reply.RecordSeq)
						if e.Branch != "bg_actual_child" || fact.Text != "child complete body" || fact.CallID != tools.CallID(ctx) {
							err = errors.New("child source fact borrowed frozen/latest parent owner")
						}
					}
					called <- err
					return "completed", err
				}}
			}
		}
		return list
	}
	task, err := k.StartSubagent(context.Background(), SubagentRequest{Type: "dev", Task: "send child peer message"})
	if err != nil {
		t.Fatal(err)
	}
	if done := waitEvent(t, box, exec.EvDone); done.Sender != task.ID {
		t.Fatal("completion belongs to another child")
	}
	select {
	case err := <-called:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("actual child never called peer-message over HTTP")
	}
}
