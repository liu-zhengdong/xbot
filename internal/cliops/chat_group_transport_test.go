package cliops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestChatGroupRuntimeTransportWire(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-XBot-Root") != canonical {
			t.Errorf("root=%q", r.Header.Get("X-XBot-Root"))
		}
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1/chat-group/list":
			if r.Method != "GET" || len(raw) != 0 || r.Header.Get("X-XBot-Call-ID") != "" {
				t.Errorf("list request=%+v", r)
			}
			io.WriteString(w, `{"groups":[],"count":0}`)
		case "/v1/chat-group/history":
			if r.Method != "GET" || len(raw) != 0 || r.Header.Get("X-XBot-Call-ID") != "" || r.URL.Query().Get("id") != "g /?" || r.URL.Query().Get("keyword") != "<&> +\n" || r.URL.Query().Get("before_id") != "gm&" || r.URL.Query().Get("limit") != "-1" || r.URL.Query().Get("sender") != "B" {
				t.Errorf("history query=%s body=%q", r.URL.RawQuery, raw)
			}
			io.WriteString(w, `{"messages":[],"untrusted":true,"count":0}`)
		case "/v1/chat-group/create":
			if r.Method != "POST" || string(raw) != `{"name":"room","members":["B","C"]}` || r.Header.Get("X-XBot-Call-ID") != "" {
				t.Errorf("create method=%s body=%s", r.Method, raw)
			}
			io.WriteString(w, `{"created":true}`)
		case "/v1/chat-group/post":
			if r.Method != "POST" || string(raw) != `{"id":"g","text":"body"}` || r.Header.Get("X-XBot-Call-ID") != "original" {
				t.Errorf("post method=%s body=%s call=%q", r.Method, raw, r.Header.Get("X-XBot-Call-ID"))
			}
			w.WriteHeader(400)
			io.WriteString(w, `{"id":"g","message_id":"gm","appended":true,"posted_members":["B"],"failed_member":"C","stage":"recipient_open","error":"late failure"}`)
		default:
			t.Errorf("path=%s", r.URL.Path)
		}
	}))
	defer source.Close()
	ctx := context.Background()
	if _, err := CallChatGroupList(ctx, source.URL, root); err != nil {
		t.Fatal(err)
	}
	if _, err := CallChatGroupHistory(ctx, source.URL, root, ChatGroupHistory{ID: "g /?", Limit: -1, BeforeID: "gm&", Sender: "B", Keyword: "<&> +\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := CallChatGroupCreate(ctx, source.URL, root, ChatGroupCreate{Name: "room", Members: []string{"B", "C"}}); err != nil {
		t.Fatal(err)
	}
	post, err := CallChatGroupPost(ctx, source.URL, root, "original", ChatGroupPost{ID: "g", Text: "body"})
	if err == nil || !post.Appended || post.MessageID != "gm" || !reflect.DeepEqual(post.PostedMembers, []string{"B"}) || post.FailedMember != "C" || calls != 4 {
		t.Fatalf("partial=%+v calls=%d err=%v", post, calls, err)
	}
}

func TestChatGroupRuntimeTransportStrictBoundary(t *testing.T) {
	calls := 0
	handlers := &ChatGroupHandlers{
		History: func(context.Context, ChatGroupHistory) (ChatGroupHistoryReply, error) {
			calls++
			lines := []ChatGroupLine{}
			return ChatGroupHistoryReply{Messages: &lines, Untrusted: true}, nil
		},
		Create: func(context.Context, ChatGroupCreate) (ChatGroupCreateReply, error) {
			calls++
			return ChatGroupCreateReply{Created: true}, nil
		},
		Post: func(ctx context.Context, input ChatGroupPost) (ChatGroupPostReply, error) {
			calls++
			return ChatGroupPostReply{ID: input.ID, Appended: true, PostedMembers: []string{"B"}, Stage: "recipient_open"}, errors.New("late failure")
		},
	}
	for _, raw := range []string{"null", "[]", `"x"`, `{} {}`, `{} true`, `{"text":true}`, `{"id":"g","text":"x","sender":"fake"}`, strings.Repeat(" ", ChatGroupMaxRequest+1), `{"text":"` + strings.Repeat("<", ChatGroupMaxRequest/6+1) + `"}`} {
		w := httptest.NewRecorder()
		serveChatGroup(w, httptest.NewRequest("POST", "/v1/chat-group/post", strings.NewReader(raw)), handlers)
		if w.Code != 400 || calls != 0 {
			t.Fatalf("bytes=%d code=%d calls=%d", len(raw), w.Code, calls)
		}
	}
	for _, query := range []string{"", "id=g&id=h", "id=g&limit=oops", "id=g&limit=", "id=g&unknown=x", "id=g&sender=A&sender=B", "id=%zz"} {
		w := httptest.NewRecorder()
		serveChatGroup(w, httptest.NewRequest("GET", "/v1/chat-group/history?"+query, nil), handlers)
		if w.Code != 400 || calls != 0 {
			t.Fatalf("query=%s code=%d calls=%d", query, w.Code, calls)
		}
	}
	for _, action := range []string{"create", "post"} {
		w := httptest.NewRecorder()
		serveChatGroup(w, httptest.NewRequest("GET", "/v1/chat-group/"+action, nil), handlers)
		if w.Code != 405 {
			t.Fatalf("method code=%d", w.Code)
		}
	}
	// Neither empty/oversized text nor names/members are domain-validated here.
	for _, raw := range []string{`{"name":"","members":[]}`, `{"name":"` + strings.Repeat("n", 100) + `","members":[""]}`} {
		w := httptest.NewRecorder()
		serveChatGroup(w, httptest.NewRequest("POST", "/v1/chat-group/create", strings.NewReader(raw)), handlers)
		if w.Code != 200 {
			t.Fatalf("domain create code=%d", w.Code)
		}
	}
	base := `{"id":"g","text":""}`
	exact := `{"id":"g","text":"` + strings.Repeat("x", ChatGroupMaxRequest-len(base)) + `"}`
	for _, raw := range []string{exact, exact + " "} {
		w := httptest.NewRecorder()
		serveChatGroup(w, httptest.NewRequest("POST", "/v1/chat-group/post", strings.NewReader(raw)), handlers)
		if len(raw) == ChatGroupMaxRequest {
			var reply ChatGroupPostReply
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || !reply.Appended || len(reply.PostedMembers) != 1 {
				t.Fatalf("receipt=%s err=%v", w.Body, err)
			}
		} else if w.Code != 400 {
			t.Fatalf("max+1 code=%d", w.Code)
		}
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestChatGroupRuntimeTransportGuardsAndBudgets(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	source, err := Start(root, Handlers{ChatGroups: &ChatGroupHandlers{List: func(context.Context) (ChatGroupListReply, error) {
		calls++
		groups := []ChatGroup{}
		return ChatGroupListReply{Groups: &groups}, nil
	}, Post: func(ctx context.Context, input ChatGroupPost) (ChatGroupPostReply, error) {
		calls++
		if SourceCallID(ctx) != "live" {
			t.Errorf("call=%q", SourceCallID(ctx))
		}
		return ChatGroupPostReply{ID: input.ID, Appended: true, PostedMembers: []string{"B"}, Stage: "complete"}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	for _, kind := range []string{"origin", "fetch", "host", "root"} {
		req, _ := http.NewRequest("GET", source.Endpoint+"/v1/chat-group/list", nil)
		req.Header.Set("X-XBot-Root", canonical)
		switch kind {
		case "origin":
			req.Header.Set("Origin", "https://example.com")
		case "fetch":
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		case "host":
			req.Host = "localhost:123"
		case "root":
			req.Header.Set("X-XBot-Root", t.TempDir())
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode < 400 || calls != 0 {
			t.Fatalf("guard=%s code=%d calls=%d", kind, response.StatusCode, calls)
		}
	}
	if _, err := CallChatGroupList(context.Background(), source.Endpoint, root); err != nil {
		t.Fatal(err)
	}
	if _, err := CallChatGroupPost(context.Background(), source.Endpoint, root, "live", ChatGroupPost{ID: "g", Text: ""}); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://example.com:80", "http://localhost:80", "https://127.0.0.1:80", source.Endpoint + "/bad"} {
		if _, err := CallChatGroupList(context.Background(), endpoint, root); err == nil {
			t.Fatalf("accepted endpoint=%s", endpoint)
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	// A large diagnostic must not erase the source's actual write facts.
	w := httptest.NewRecorder()
	serveChatGroup(w, httptest.NewRequest("POST", "/v1/chat-group/post", strings.NewReader(`{"id":"g","text":"body"}`)), &ChatGroupHandlers{Post: func(context.Context, ChatGroupPost) (ChatGroupPostReply, error) {
		return ChatGroupPostReply{ID: "g", MessageID: "gm", Appended: true, PostedMembers: []string{"B"}, Stage: "recipient_open"}, errors.New(strings.Repeat("e", 40<<10))
	}})
	var partial ChatGroupPostReply
	if err := json.Unmarshal(w.Body.Bytes(), &partial); err != nil || !partial.Appended || partial.MessageID != "gm" || len(partial.PostedMembers) != 1 || w.Body.Len() > 32<<10 {
		t.Fatalf("partial=%+v bytes=%d err=%v", partial, w.Body.Len(), err)
	}
}
