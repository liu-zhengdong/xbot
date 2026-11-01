package cliops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutineTransportTypedCRUDAndPartial(t *testing.T) {
	root := t.TempDir()
	empty := []RoutineView{}
	calls := make(chan string, 4)
	source, err := Start(root, Handlers{Routines: &RoutineHandlers{
		Create: func(ctx context.Context, in RoutineCreate) (RoutineWriteReply, error) {
			calls <- "create"
			if RoutineCallID(ctx) != "source-call" || in.ID != "r1" || in.Precheck != nil || in.Prompt != " raw " {
				t.Errorf("create input=%+v call=%q", in, RoutineCallID(ctx))
			}
			return RoutineWriteReply{ID: in.ID, Applied: true, Stage: "complete"}, nil
		},
		List: func(ctx context.Context) (RoutineListReply, error) {
			calls <- "list"
			if RoutineCallID(ctx) != "" {
				t.Errorf("list inherited call ID=%q", RoutineCallID(ctx))
			}
			return RoutineListReply{Routines: &empty}, nil
		},
		Update: func(ctx context.Context, id string, in RoutinePatch) (RoutineWriteReply, error) {
			calls <- "update"
			if id != "literal / ID" || RoutineCallID(ctx) != "" || in.Enabled == nil || *in.Enabled || in.Precheck == nil || *in.Precheck != "" || in.Skills == nil || len(*in.Skills) != 0 {
				t.Errorf("update id=%q input=%+v call=%q", id, in, RoutineCallID(ctx))
			}
			return RoutineWriteReply{ID: id, Applied: true, Stage: "zone_index"}, errors.New("zone index write failed")
		},
		Delete: func(ctx context.Context, id string) (RoutineWriteReply, error) {
			calls <- "delete"
			if id != "r1" || RoutineCallID(ctx) != "delete-call" {
				t.Errorf("delete id=%q call=%q", id, RoutineCallID(ctx))
			}
			return RoutineWriteReply{ID: id, Applied: true, Stage: "complete"}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	ctx := context.Background()
	created, err := CallRoutineCreate(ctx, source.Endpoint, root, "source-call", RoutineCreate{ID: "r1", Prompt: " raw "})
	if err != nil || !created.Applied || created.ID != "r1" || created.Stage != "complete" {
		t.Fatalf("created=%+v error=%v", created, err)
	}
	listed, err := CallRoutineList(ctx, source.Endpoint, root)
	if err != nil || listed.Routines == nil || *listed.Routines == nil || len(*listed.Routines) != 0 {
		t.Fatalf("listed=%+v error=%v", listed, err)
	}
	disabled, clear, skills := false, "", []string{}
	partial, err := CallRoutineUpdate(ctx, source.Endpoint, root, "", "literal / ID", RoutinePatch{Enabled: &disabled, Precheck: &clear, Skills: &skills})
	if err == nil || err.Error() != "zone index write failed" || !partial.Applied || partial.Stage != "zone_index" || partial.ID != "literal / ID" || partial.Error != err.Error() {
		t.Fatalf("partial=%+v error=%v", partial, err)
	}
	deleted, err := CallRoutineDelete(ctx, source.Endpoint, root, "delete-call", "r1")
	if err != nil || !deleted.Applied {
		t.Fatalf("deleted=%+v error=%v", deleted, err)
	}
	if len(calls) != 4 {
		t.Fatalf("calls=%d want four, without write retries", len(calls))
	}
}

func TestRoutineTransportRequestGuards(t *testing.T) {
	calls := 0
	handlers := &RoutineHandlers{Create: func(context.Context, RoutineCreate) (RoutineWriteReply, error) {
		calls++
		return RoutineWriteReply{Applied: true, Stage: "complete"}, nil
	}}
	for _, raw := range []string{"null", "[]", `{} {}`, `{"unknown":1}`, `{"precheck":true}`, "\u00a0{}", "{}\u00a0", strings.Repeat(" ", RoutineMaxRequest+1)} {
		r := httptest.NewRequest(http.MethodPost, "/v1/routine/create", strings.NewReader(raw))
		w := httptest.NewRecorder()
		serveRoutine(w, r, handlers)
		if w.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("invalid request reached domain: length=%d status=%d calls=%d", len(raw), w.Code, calls)
		}
	}
	for _, path := range []string{"create", "update", "delete"} {
		w := httptest.NewRecorder()
		serveRoutine(w, httptest.NewRequest(http.MethodGet, "/v1/routine/"+path, nil), handlers)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("wrong method for %s: status=%d", path, w.Code)
		}
	}
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	source, err := Start(root, Handlers{Routines: handlers})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	for _, tc := range []struct{ header, value string }{
		{"X-XBot-Root", "wrong root"}, {"Origin", "http://localhost"}, {"Sec-Fetch-Site", "same-origin"}, {"Host", "localhost:1234"},
	} {
		req, _ := http.NewRequest(http.MethodPost, source.Endpoint+"/v1/routine/create", strings.NewReader(`{}`))
		req.Header.Set("X-XBot-Root", canonical)
		if tc.header == "Host" {
			req.Host = tc.value
		} else {
			req.Header.Set(tc.header, tc.value)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 || calls != 0 {
			t.Fatalf("guard %s: status=%d calls=%d", tc.header, resp.StatusCode, calls)
		}
	}
	if values := (Caller{Binary: "bin", Root: "root", Endpoint: "endpoint", Bot: "bot"}).Env([]string{"XBOT_CALL_ID=old-call", "OTHER=value"}); len(values) != 5 || values[0] != "OTHER=value" {
		t.Fatalf("inherited call ID survived caller env: %q", values)
	}
}

func TestRoutineTransportReplyBudgets(t *testing.T) {
	root := t.TempDir()
	views := []RoutineView{{ID: "r1", Prompt: ""}}
	reply := RoutineListReply{Routines: &views, Count: 1}
	base, _ := json.Marshal(reply)
	views[0].Prompt = strings.Repeat("x", RoutineMaxReply-len(base)-1)
	w := httptest.NewRecorder()
	serveRoutine(w, httptest.NewRequest(http.MethodGet, "/v1/routine/list", nil), &RoutineHandlers{List: func(context.Context) (RoutineListReply, error) { return reply, nil }})
	if w.Code != http.StatusOK || w.Body.Len() != RoutineMaxReply || !strings.HasSuffix(w.Body.String(), "\n") {
		t.Fatalf("exact reply budget: status=%d length=%d", w.Code, w.Body.Len())
	}
	views[0].Prompt += "x"
	w = httptest.NewRecorder()
	serveRoutine(w, httptest.NewRequest(http.MethodGet, "/v1/routine/list", nil), &RoutineHandlers{List: func(context.Context) (RoutineListReply, error) { return reply, nil }})
	if w.Code != http.StatusInternalServerError || w.Body.Len() > RoutineMaxReply {
		t.Fatalf("newline overflow: status=%d length=%d", w.Code, w.Body.Len())
	}
	for _, size := range []int{RoutineMaxReply, RoutineMaxReply + 1} {
		t.Run(string(rune(size-RoutineMaxReply+'0')), func(t *testing.T) {
			body := `{"count":0}` + strings.Repeat(" ", size-len(`{"count":0}`))
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
			defer source.Close()
			_, err := CallRoutineList(context.Background(), source.URL, root)
			if (err == nil) != (size == RoutineMaxReply) {
				t.Fatalf("response size=%d error=%v", size, err)
			}
		})
	}
}
