// Package notable records a small, fixed list of completed effects for reports.
// It observes execution; it does not approve or prevent any action.
package notable

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"xbot/internal/review"
)

// Item keeps the full object list separate from its short, owner-facing text.
type Item struct {
	Kind       string   `json:"kind"`
	Text       string   `json:"text"`
	Objects    []string `json:"objects,omitempty"`
	Calls      int      `json:"calls,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	Incomplete bool     `json:"incomplete,omitempty"`
}

var catalog = [...]Item{
	{Kind: "delete", Text: "删除了文件或记录", Unit: "项"},
	{Kind: "external_message", Text: "向外部服务发送了消息"},
	{Kind: "write_outside", Text: "在工作区以外写入了文件", Unit: "个文件"},
	{Kind: "global_install", Text: "全局安装了软件", Unit: "个软件"},
}

type effect struct {
	seen       bool
	objects    map[string]bool
	calls      int
	unit       string
	incomplete bool
}

// Log keeps four kinds and at most 64 KiB of object details per task. If the
// detail bound is reached, Incomplete makes the omission visible rather than
// inventing an exact count or growing task metadata without limit.
type Log struct {
	mu      sync.Mutex
	effects [len(catalog)]effect
	bytes   int
}

func (l *Log) merge(item Item) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, c := range catalog {
		if c.Kind != item.Kind {
			continue
		}
		e := &l.effects[i]
		e.seen = true
		e.calls += item.Calls
		e.incomplete = e.incomplete || item.Incomplete
		if e.unit == "" {
			e.unit = item.Unit
		} else if item.Unit != "" && e.unit != item.Unit {
			e.unit = "项"
		}
		if e.objects == nil {
			e.objects = map[string]bool{}
		}
		for _, object := range item.Objects {
			object = strings.TrimSpace(object)
			if object == "" || e.objects[object] {
				continue
			}
			if l.bytes+len(object) > 64<<10 {
				e.incomplete = true
				continue
			}
			e.objects[object] = true
			l.bytes += len(object)
		}
		return
	}
}
func (l *Log) Items() []Item {
	l.mu.Lock()
	defer l.mu.Unlock()
	var items []Item
	for i, e := range l.effects {
		if !e.seen {
			continue
		}
		item := catalog[i]
		item.Calls = e.calls
		if e.unit != "" {
			item.Unit = e.unit
		}
		item.Incomplete = e.incomplete
		for object := range e.objects {
			item.Objects = append(item.Objects, object)
		}
		sort.Strings(item.Objects)
		item.Text = itemText(item)
		items = append(items, item)
	}
	return items
}

// Ordered also normalizes older task metadata at the API/report boundary.
func Ordered(items []Item) []Item {
	var out []Item
	for _, c := range catalog {
		for _, item := range items {
			if item.Kind == c.Kind {
				item.Text = strings.TrimSuffix(strings.TrimSpace(item.Text), "。")
				out = append(out, item)
			}
		}
	}
	return out
}

// Line is the one-sentence digest given to the main bot, not the full task list.
func Line(items []Item) string {
	items = Ordered(items)
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return "值得留意：" + items[0].Text + "。"
	}
	first := items[0]
	text := first.Text
	if first.Kind == "delete" && len(first.Objects) > 0 && !first.Incomplete {
		unit := first.Unit
		if unit == "" {
			unit = "项"
		}
		text = "删了 " + strconv.Itoa(len(first.Objects)) + " " + unit
	}
	return "值得留意：" + text + "，另有 " + strconv.Itoa(len(items)-1) + " 项，详见任务。"
}

type key struct{}
type scope struct {
	workspace string
	log       *Log
}

func With(ctx context.Context, workspace string, log *Log) context.Context {
	if workspace != "" {
		if real, err := filepath.EvalSymlinks(workspace); err == nil {
			workspace = real
		}
		workspace = filepath.Clean(workspace)
	}
	return context.WithValue(ctx, key{}, scope{workspace, log})
}

// Capture stages facts for one tool invocation. The returned commit is called
// only after its outer wrapper succeeds, so rejected/rolled-back writes vanish.
func Capture(ctx context.Context) (context.Context, func()) {
	parent := from(ctx)
	if parent.log == nil {
		return ctx, nil
	}
	call := &Log{}
	return context.WithValue(ctx, key{}, scope{parent.workspace, call}), func() {
		for _, item := range call.Items() {
			parent.log.merge(item)
		}
	}
}

func from(ctx context.Context) scope { s, _ := ctx.Value(key{}).(scope); return s }
func record(ctx context.Context, kind string, objects ...string) {
	if s := from(ctx); s.log != nil {
		s.log.merge(Item{Kind: kind, Objects: objects, Calls: 1, Incomplete: len(objects) == 0})
	}
}

// Wrote takes the real resolved path from the successful file operation.
func Wrote(ctx context.Context, path string) {
	s := from(ctx)
	if s.log == nil || s.workspace == "" || !filepath.IsAbs(path) {
		return
	}
	rel, err := filepath.Rel(s.workspace, path)
	if err == nil && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		record(ctx, "write_outside", displayPath(path, ""))
	}
}

// Tool observes successful calls whose meaning is already defined by review.
// File and shell facts are reported by their implementations with actual paths.
func Tool(ctx context.Context, name string, args json.RawMessage, out string, readOnlyHint bool) {
	if from(ctx).log == nil || strings.HasPrefix(strings.TrimSpace(out), "error:") {
		return
	}
	switch review.ClassOfMCP(name, readOnlyHint) {
	case review.Delete:
		objects, unit := deletedObjects(name, args)
		recordDelete(ctx, objects, unit)
	case review.ExternalSend:
		if name != "links" {
			record(ctx, "external_message", sendObjects(name, args)...)
		}
	case review.HTTP:
		var in struct {
			Method string `json:"method"`
			URL    string `json:"url"`
		}
		if json.Unmarshal(args, &in) != nil {
			return
		}
		method := strings.ToUpper(in.Method)
		if method == "" || method == "GET" || method == "HEAD" || method == "OPTIONS" {
			return
		}
		// HTTPRequest.Run returns resp.Status followed by the response body.
		fields := strings.Fields(out)
		if len(fields) == 0 {
			return
		}
		code, err := strconv.Atoi(fields[0])
		if err == nil && code >= 200 && code < 300 {
			record(ctx, "external_message", endpoint(in.URL)...)
		}
	}
}
