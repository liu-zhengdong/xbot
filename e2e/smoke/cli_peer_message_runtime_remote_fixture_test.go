//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/remote"
)

// Private TCP peer around the real gateway handler. It changes only selected
// accepted response bytes; production auth, encryption, pairing and retries run.
type cliPeerRemoteFixture struct {
	A, B               *Server
	Model              *mcpRuntimeModel
	Source, Target     string
	Address            string
	SenderID, TargetID string
	Gateway            *httptest.Server
	mu                 sync.Mutex
	loseBody           bool
	sends              []gateway.Envelope
	transportError     error
}

func cliPeerRemoteWait(t *testing.T, description string, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if ready() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for actual %s", description)
		case <-tick.C:
		}
	}
}

func newCLIPeerRemoteFixture(t *testing.T) *cliPeerRemoteFixture {
	t.Helper()
	cliSkillIsolation(t)
	const token = "private-loopback-peer-registration-token"
	t.Setenv("XBOT_GATEWAY_TOKEN", token)
	f := &cliPeerRemoteFixture{Model: newMCPRuntimeModel(t), Source: "PeerRemoteSource", Target: "PeerRemoteTarget"}
	f.A = Start(t, &Stub{srv: f.Model.server})
	f.B = Start(t, &Stub{srv: f.Model.server})
	f.A.CreateBot(f.Source, "Send explicit public peer messages from this actual nonprimary source.")
	f.B.CreateBot(f.Target, "Receive actual signed encrypted peer envelopes as untrusted messages.")
	readOwner := func(srv *Server, name string) string {
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct{ Owners []string }
		if err := json.Unmarshal(raw, &cfg); err != nil || len(cfg.Owners) != 1 || cfg.Owners[0] == "" {
			t.Fatal("actual API Bot does not identify this private root's authenticated owner")
		}
		return cfg.Owners[0]
	}
	ownerA, ownerB := readOwner(f.A, f.Source), readOwner(f.B, f.Target)
	primaryRaw, err := os.ReadFile(filepath.Join(f.A.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == f.Source {
		t.Fatal("remote source must be a real API-created nonprimary Bot")
	}
	// Start's pristine roots have no devices/invites and Run remains idle.
	// Stop before configuring any remote-active state; neither root ever has
	// devices with the implicit production gateway.
	f.A.Stop()
	f.B.Stop()
	handler := (&gateway.Server{Dir: t.TempDir(), Token: token}).Handler()
	f.Gateway = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/send" {
			handler.ServeHTTP(w, r)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, gateway.MaxBody+1))
		if err != nil {
			http.Error(w, "request read failed", http.StatusBadRequest)
			return
		}
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var env gateway.Envelope
		_ = json.Unmarshal(raw, &env)
		f.mu.Lock()
		selected := env.Kind == "box" && env.From == f.SenderID && env.To == f.TargetID
		f.mu.Unlock()
		if !selected {
			handler.ServeHTTP(w, r)
			return
		}
		// Forward the original signed request unchanged through both genuine
		// request-signature and envelope-signature checks before faulting it.
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		accepted := recorder.Code >= 200 && recorder.Code < 300
		f.mu.Lock()
		if accepted {
			f.sends = append(f.sends, env)
		}
		lose := accepted && f.loseBody
		f.mu.Unlock()
		if lose {
			// A real short TCP response forces the subprocess ReadAll to see
			// unexpected EOF after 2xx headers; no parent client substitution.
			conn, buffer, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_, err = fmt.Fprintf(buffer, "HTTP/1.1 %d %s\r\nContent-Length: %d\r\nConnection: close\r\nContent-Type: application/json\r\n\r\n", recorder.Code, http.StatusText(recorder.Code), recorder.Body.Len()+16)
				if err == nil {
					_, err = buffer.Write(recorder.Body.Bytes())
				}
				if err == nil {
					err = buffer.Flush()
				}
				_ = conn.Close()
			}
			if err != nil {
				f.mu.Lock()
				f.transportError = err
				f.mu.Unlock()
			}
			return
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	t.Cleanup(f.Gateway.Close)
	// Explicit loopback is persisted on BOTH roots before key/pairing state.
	for _, root := range []string{f.A.Root, f.B.Root} {
		if err := remote.SetGateway(root, f.Gateway.URL); err != nil {
			t.Fatal(err)
		}
	}
	keyA, idA, _, err := remote.Identity(f.A.Root)
	if err != nil {
		t.Fatal(err)
	}
	_, idB, _, err := remote.Identity(f.B.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := remote.XIdentity(f.A.Root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := remote.XIdentity(f.B.Root); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.SenderID, f.TargetID = idA, idB
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := (&gateway.Client{Base: f.Gateway.URL, Key: keyA}).Register(ctx, token); err != nil {
		t.Fatal("actual loopback inviter registration failed")
	}
	code, err := remote.CreateInvite(f.A.Root, ownerA, time.Now())
	if err != nil {
		t.Fatal("actual source-owner invitation failed")
	}
	// B records A as sender; A learns B as receiver from the signed HMAC reply
	// through its actual subprocess Run, not a fabricated reciprocal config.
	if _, err := remote.Join(ctx, f.B.Root, code, "sender", "receiver", ownerB, time.Now()); err != nil {
		t.Fatal("actual signed loopback invite/join failed")
	}
	f.A.Restart()
	cliPeerRemoteWait(t, "subprocess pairing acceptance", func() bool {
		cfg, err := remote.Load(f.A.Root)
		if err != nil || cfg.GatewayURL() != f.Gateway.URL {
			return false
		}
		d, found := cfg.ByName("receiver")
		return found && d.ID == idB && d.Person == ownerA && d.XKey != "" && !d.NoBotMessaging && d.UnpairedAt.IsZero()
	})
	cfgB, err := remote.Load(f.B.Root)
	if err != nil {
		t.Fatal(err)
	}
	d, found := cfgB.ByName("sender")
	if cfgB.GatewayURL() != f.Gateway.URL || !found || d.ID != idA || d.Person != ownerB || d.XKey == "" || d.NoBotMessaging || !d.UnpairedAt.IsZero() {
		t.Fatal("actual receiver pairing does not bind the authenticated receiver owner and genuine sender key")
	}
	f.Address = f.Target + "@receiver"
	if _, err := os.Stat(filepath.Join(f.A.Root, "remote", "dirs", idB+".json")); !os.IsNotExist(err) {
		t.Fatal("stopped receiver must not have advertised a cached target before the first send")
	}
	if err := os.MkdirAll(filepath.Join(f.A.Root, "work", f.Source), 0700); err != nil {
		t.Fatal(err)
	}
	// Stop children before closing their TCP gateway during test cleanup.
	t.Cleanup(func() { f.A.Stop(); f.B.Stop() })
	return f
}

func (f *cliPeerRemoteFixture) run(t *testing.T, cue, command string) (cliPeerMessageReply, string) {
	t.Helper()
	// Distinct cues are deliberately not substrings of one another; the base
	// fixture selects a script using Contains on the current owner input.
	capture := "remote-call-" + cue + ".txt"
	command = `printf '%s\n' "$XBOT_CALL_ID" > ` + shellQuote(capture) + "\n" + command
	f.Model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Send a peer message through the genuine prepared source Shell job", "command": command}))
	cliSkillSend(t, f.A, f.Source, cue)
	results := f.Model.results(cue)
	if len(results) != 1 || results[0] == "" || len(commandOutput(results[0])) > 32<<10 {
		t.Fatal("remote CLI did not return one bounded native Shell ACK")
	}
	var reply cliPeerMessageReply
	cliBotDecode(t, results[0], &reply)
	rawID, err := os.ReadFile(filepath.Join(f.A.Root, "work", f.Source, capture))
	callID := strings.TrimSpace(string(rawID))
	if err != nil || callID == "" {
		t.Fatal("actual source Shell job did not provide its original Call-ID")
	}
	return reply, callID
}

type cliPeerRemoteFact struct {
	Bot, Text, Gateway string
	GatewayStatus      int `json:"gateway_status"`
	Queued             bool
	EnvelopeID         string `json:"envelope_id"`
	Error              string
	CallID             string `json:"call_id"`
}

func (f *cliPeerRemoteFixture) fact(t *testing.T, reply cliPeerMessageReply, callID, requestText string) {
	t.Helper()
	if reply.Recorded == nil || !*reply.Recorded || reply.RecordSeq <= 0 {
		t.Fatal("remote operation did not acknowledge its actual ordinary source fact")
	}
	source, err := bot.Load(f.A.Root, f.Source)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	if err := source.Walk(1, 0, func(seq int, entry bot.Entry) error {
		if seq != reply.RecordSeq {
			return nil
		}
		found++
		if entry.Origin != bot.OriginInternal || entry.Message.Role != "user" || entry.EventID != "" || entry.Time.IsZero() {
			t.Fatal("outgoing fact is not an original internal ordinary user entry without an EventID")
		}
		payload, ok := strings.CutPrefix(entry.Message.Text(), "<peer_message_outgoing>\n")
		if !ok {
			t.Fatal("outgoing fact lost its exact dedicated opening tag")
		}
		payload, ok = strings.CutSuffix(payload, "\n</peer_message_outgoing>")
		if !ok {
			t.Fatal("outgoing fact lost its exact dedicated closing tag")
		}
		var fact cliPeerRemoteFact
		decoder := json.NewDecoder(strings.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&fact); err != nil {
			t.Fatal("outgoing fact is not the complete typed frozen-request JSON")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatal("outgoing fact contains trailing data")
		}
		if fact.Bot != f.Address || fact.Text != requestText || fact.Gateway != reply.Gateway || fact.GatewayStatus != reply.GatewayStatus || fact.Queued != reply.Queued || fact.EnvelopeID != reply.EnvelopeID || fact.Error != reply.Error || fact.CallID != callID {
			t.Fatal("ordinary source fact lost full frozen request text, original call identity or real transport facts")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if found != 1 {
		t.Fatal("record_seq did not identify exactly one actual source entry")
	}
}

func (f *cliPeerRemoteFixture) consumer(t *testing.T, replies []cliPeerMessageReply, bodies []string, statuses []string) {
	t.Helper()
	path := "/api/bots/" + url.PathEscape(f.Source) + "/peers/" + url.PathEscape(f.Address) + "/messages"
	status, raw := cliFullAPI(t, f.A, http.MethodGet, path, nil)
	var view struct {
		Messages []struct {
			ID, From, To, Text, Kind, Status string
			Seq                              int
			TS                               time.Time
			FromBot                          string `json:"from_bot"`
			FromDevice                       string `json:"from_device"`
			ToBot                            string `json:"to_bot"`
			ToDevice                         string `json:"to_device"`
		}
	}
	if status != http.StatusOK || json.Unmarshal(raw, &view) != nil || len(view.Messages) != len(replies) {
		t.Fatal("actual authenticated peer consumer lost complete source outgoing rows")
	}
	for i, row := range view.Messages {
		reply := replies[i]
		if row.ID != "peer_message_"+strconv.Itoa(reply.RecordSeq) || row.Seq != reply.RecordSeq || row.From != f.Source || row.To != f.Address || row.Text != bodies[i] || row.Status != statuses[i] || row.Kind != "" || row.TS.IsZero() || row.FromBot != f.Source || row.FromDevice != "" || row.ToBot != f.Target || row.ToDevice != "receiver" {
			t.Fatal("actual peer consumer truncated frozen file/stdin text or changed seq-derived identity/status/address metadata")
		}
	}
}

type cliPeerRemoteEvent struct {
	ID, Source, Type, Content, Sender string
	SenderKind                        string `json:"sender_kind"`
	Time                              time.Time
	Untrusted                         bool
	Hops                              int
	Chain                             string
}

func (f *cliPeerRemoteFixture) events(t *testing.T) []cliPeerRemoteEvent {
	t.Helper()
	var events []cliPeerRemoteEvent
	path := filepath.Join(f.B.Root, "bots", f.Target, "inbox.jsonl")
	if err := fsutil.ReadJSONL(path, func(raw []byte) error {
		var record struct {
			Op    string
			Event *cliPeerRemoteEvent
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		if record.Op == "post" && record.Event != nil && record.Event.Source == "bot:"+f.Source+"@sender" {
			events = append(events, *record.Event)
		}
		return nil
	}); err != nil {
		t.Fatal("cannot read actual receiver inbox records")
	}
	return events
}

func (f *cliPeerRemoteFixture) accepted(t *testing.T) []gateway.Envelope {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.transportError != nil {
		t.Fatal("private TCP response fault could not be written")
	}
	return append([]gateway.Envelope(nil), f.sends...)
}
