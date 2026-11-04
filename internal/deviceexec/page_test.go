package deviceexec

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskPagesCoverAllHistoryWithStableOrdering(t *testing.T) {
	m := testManager(t)
	s := testSource()
	m.mu.Lock()
	c, err := m.contextLocked(s)
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	frozen := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	var tick atomic.Int64
	c.jobs.Now = func() time.Time { return frozen.Add(time.Duration(tick.Load()) * time.Second) }
	for i := 0; i < 205; i++ {
		ref := fmt.Sprintf("page-%03d", i)
		submit(t, m, s, testCall(ref, "read", "read_file", map[string]any{"path": "missing"}))
		waitDetail(t, m, s, ref)
	}
	if len(m.List(&s)) != 200 {
		t.Fatal("legacy recent list changed")
	}
	first := m.ListPage(&s, 0, 0)
	if first.Offset != 0 || first.NextOffset != 10 || !first.HasMore || len(first.Tasks) != 10 || first.Tasks[0].Ref != "page-000" || first.Tasks[9].Ref != "page-009" {
		t.Fatalf("default page: %+v", first)
	}
	last := m.ListPage(&s, 200, 10)
	if last.Offset != 200 || last.NextOffset != 205 || last.HasMore || len(last.Tasks) != 5 || last.Tasks[0].Ref != "page-200" || last.Tasks[4].Ref != "page-204" {
		t.Fatalf("historical page: %+v", last)
	}
	if page := m.ListPage(&s, 0, 1000); len(page.Tasks) != 200 || page.NextOffset != 200 || !page.HasMore {
		t.Fatalf("limit clamp: %+v", page)
	}
	if page := m.ListPage(&s, 999, 10); len(page.Tasks) != 0 || page.NextOffset != 999 || page.HasMore {
		t.Fatalf("past end: %+v", page)
	}
	// A later task must sort ahead of the tied records.
	tick.Store(1)
	submit(t, m, s, testCall("newest", "read", "read_file", map[string]any{"path": "missing"}))
	waitDetail(t, m, s, "newest")
	if page := m.ListPage(&s, 0, 1); page.Tasks[0].Ref != "newest" {
		t.Fatalf("not newest first: %+v", page)
	}
}

func TestHTTPTaskPagesRespectBudgetAndOwnerFilters(t *testing.T) {
	m := testManager(t)
	s := testSource()
	for i := 0; i < 32; i++ {
		call := testCall(fmt.Sprintf("large-page-%02d", i), "read", "read_file", map[string]any{"path": "missing"})
		call.Description = strings.Repeat("\x01", 1024)
		submit(t, m, s, call)
		waitDetail(t, m, s, call.Ref)
	}
	child := s
	child.ContextID = "child"
	otherDevice := s
	otherDevice.DeviceID = "other-installation"
	otherBot := s
	otherBot.Bot = "beta"
	otherAccount := s
	otherAccount.Account = "other-account"
	for i, source := range []Source{child, otherDevice, otherBot, otherAccount} {
		ref := fmt.Sprintf("foreign-page-%d", i)
		submit(t, m, source, testCall(ref, "read", "read_file", map[string]any{"path": "missing"}))
		waitDetail(t, m, source, ref)
	}
	owner := Source{Account: s.Account, DeviceID: "console", Bot: "owner", ContextID: "owner", Owner: true}
	seen := map[string]bool{}
	offset := int64(0)
	for {
		path := "/tasks?bot=alpha&source_device_id=origin&limit=200&offset=" + strconv.FormatInt(offset, 10)
		response := request(m, &owner, http.MethodGet, path, nil)
		if response.Code != http.StatusOK || response.Body.Len() > MaxTaskPageBytes {
			t.Fatalf("page budget: status=%d bytes=%d", response.Code, response.Body.Len())
		}
		var page TaskPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Offset != offset || page.NextOffset != offset+int64(len(page.Tasks)) || len(page.Tasks) == 0 {
			t.Fatalf("page progress: %+v", page)
		}
		for _, task := range page.Tasks {
			if task.Bot != "alpha" || task.Ref == "foreign-page-1" || task.Ref == "foreign-page-3" || seen[task.Ref] {
				t.Fatalf("unfiltered or duplicate task: %+v", task)
			}
			seen[task.Ref] = true
		}
		if !page.HasMore {
			break
		}
		offset = page.NextOffset
	}
	if len(seen) != 33 {
		t.Fatalf("paging omitted tasks: %d", len(seen))
	}
	if !seen["foreign-page-0"] {
		t.Fatal("owner filter lost child context")
	}
	// Ordinary sources cannot broaden scope through owner query parameters.
	response := request(m, &s, http.MethodGet, "/tasks?bot=beta&source_device_id=other-installation&limit=1", nil)
	var page TaskPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(page.Tasks) != 1 || page.Tasks[0].Bot != "alpha" || page.Tasks[0].ContextID != "main" || page.NextOffset != 1 {
		t.Fatalf("source broadened scope: %+v", page)
	}
	for _, query := range []string{"offset=-1", "offset=invalid", "limit=-1"} {
		if response := request(m, &s, http.MethodGet, "/tasks?"+query, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("bad pagination %s: %d", query, response.Code)
		}
	}
}
