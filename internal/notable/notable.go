// Package notable records a small, fixed list of completed effects for reports.
// It observes execution; it does not approve or prevent any action.
package notable

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"xbot/internal/review"
)

type Item struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

var catalog = [...]Item{
	{"global_install", "全局安装了软件。"},
	{"write_outside", "在工作区以外写入了文件。"},
	{"external_message", "向外部服务发送了消息。"},
	{"delete", "删除了文件或记录。"},
}

// Log stores at most four effects, one of each kind. Zero value is ready to use.
type Log struct {
	mu    sync.Mutex
	kinds [len(catalog)]bool
}

func (l *Log) add(kind string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, item := range catalog {
		if item.Kind == kind {
			l.kinds[i] = true
			return
		}
	}
}
func (l *Log) Items() []Item {
	l.mu.Lock()
	defer l.mu.Unlock()
	var items []Item
	for i, yes := range l.kinds {
		if yes {
			items = append(items, catalog[i])
		}
	}
	return items
}

// Line is shared by the task report and the payload the main bot receives.
func Line(items []Item) string {
	if len(items) == 0 {
		return ""
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		texts = append(texts, item.Text)
	}
	return "值得留意：" + strings.Join(texts, " ")
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
			parent.log.add(item.Kind)
		}
	}
}

func from(ctx context.Context) scope { s, _ := ctx.Value(key{}).(scope); return s }
func record(ctx context.Context, kind string) {
	if s := from(ctx); s.log != nil {
		s.log.add(kind)
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
		s.log.add("write_outside")
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
		record(ctx, "delete")
	case review.ExternalSend:
		if name != "links" {
			record(ctx, "external_message")
		}
	case review.HTTP:
		var in struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(args, &in) != nil {
			return
		}
		method := strings.ToUpper(in.Method)
		if method == "" || method == "GET" || method == "HEAD" || method == "OPTIONS" {
			return
		}
		fields := strings.Fields(out)
		if len(fields) == 0 {
			return
		}
		code, err := strconv.Atoi(fields[0])
		if err == nil && code >= 200 && code < 300 {
			record(ctx, "external_message")
		}
	}
}
