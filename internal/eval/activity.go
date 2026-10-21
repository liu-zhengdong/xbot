package eval

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"xbot/internal/provider"
)

// Activity counts provider requests and tool starts before the first observed
// work-directory change. Null means no change was observed. For concurrent
// calls, the result hook is the observation point; the changing call is excluded.
type Activity struct {
	Steps                 int  `json:"steps"`
	ToolsBeforeFirstWrite *int `json:"tools_before_first_write"`
}

type activity struct {
	provider.Provider
	mu      sync.Mutex
	work    string
	initial map[string][32]byte
	stats   Activity
	calls   int
	names   map[string]bool
}

func newActivity(work string, p provider.Provider) *activity {
	return &activity{Provider: p, work: work, initial: fileContents(work), names: map[string]bool{}}
}
func (a *activity) Chat(ctx context.Context, req provider.Request, delta func(string)) (provider.Message, error) {
	a.mu.Lock()
	a.stats.Steps++
	for _, t := range req.Tools {
		a.names[t.Name] = true
	}
	a.mu.Unlock()
	return a.Provider.Chat(ctx, req, delta)
}
func (a *activity) toolStart(_, _ string) { a.mu.Lock(); a.calls++; a.mu.Unlock() }
func (a *activity) toolEnd(_, _ string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stats.ToolsBeforeFirstWrite == nil && !reflect.DeepEqual(a.initial, fileContents(a.work)) {
		n := a.calls - 1
		if n < 0 {
			n = 0
		}
		a.stats.ToolsBeforeFirstWrite = &n
	}
}
func (a *activity) result() Activity { a.mu.Lock(); defer a.mu.Unlock(); return a.stats }
func (a *activity) toolNames() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var names []string
	for n := range a.names {
		names = append(names, n)
	}
	return names
}
func fileContents(root string) map[string][32]byte {
	out := map[string][32]byte{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		// Never follow a fixture symlink out of the eval workspace.
		if d.Type()&os.ModeSymlink != 0 {
			s, e := os.Readlink(path)
			if e == nil {
				out[path] = sha256.Sum256([]byte(s))
			}
			return e
		}
		b, e := os.ReadFile(path)
		if e == nil {
			out[path] = sha256.Sum256(b)
		}
		return e
	})
	return out
}
func countText(n *int) string {
	if n == nil {
		return "n/a (no write)"
	}
	return fmt.Sprint(*n)
}
