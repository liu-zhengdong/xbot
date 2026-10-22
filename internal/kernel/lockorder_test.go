package kernel

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// lockName normalizes the receiver a lock expression hangs off, so t.mu,
// turn.mu and tr.mu all read as one lock. The kernel names one turn's mutex
// all three ways.
func lockName(expr ast.Expr) string {
	var sel string
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		inner := ""
		switch x := e.X.(type) {
		case *ast.Ident:
			inner = x.Name
		case *ast.SelectorExpr:
			inner = x.Sel.Name
		}
		sel = inner + "." + e.Sel.Name
	case *ast.Ident:
		sel = e.Name
	}
	switch sel {
	case "t.mu", "turn.mu", "tr.mu":
		return "turn.mu"
	}
	return strings.TrimPrefix(sel, "k.")
}

// TestLockOrderHasNoReverseNesting enforces the lock order documented in the
// package comment: bgMu may be held while turn.mu is taken, never the other way
// round. The reverse order deadlocks a live bot the first time a background
// finishes while the owner sends a message, and a deadlock is far cheaper to
// catch here than in production.
func TestLockOrderHasNoReverseNesting(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		checked++
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			// held is the set of locks held at this point. A deferred Unlock
			// only counts once it is released, not when it is queued, because
			// it still guards the rest of the body.
			held := map[string]bool{}
			deferred := map[string]bool{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch e := n.(type) {
				case *ast.CallExpr:
					sel, ok := e.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					name := lockName(sel.X)
					switch sel.Sel.Name {
					case "Lock":
						if name == "turn.mu" && held["bgMu"] {
							// bgMu -> turn.mu: the documented direction.
						} else if name == "bgMu" && held["turn.mu"] {
							t.Errorf("%s: %s takes turn.mu then bgMu; the kernel only allows bgMu -> turn.mu",
								fset.Position(e.Pos()), fn.Name.Name)
						}
						held[name] = true
					case "Unlock":
						if !deferred[name] {
							delete(held, name)
						}
					}
				case *ast.DeferStmt:
					sel, ok := e.Call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if sel.Sel.Name == "Unlock" {
						deferred[lockName(sel.X)] = true
					}
				}
				return true
			})
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no non-test files parsed; the guard did not run")
	}
}

// TestKernelLockCountIsKnown fails when the number of mutexes on Kernel changes,
// so adding a lock is a deliberate act that updates the package comment rather
// than something that slips in with a feature.
func TestKernelLockCountIsKnown(t *testing.T) {
	src, err := os.ReadFile("kernel.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "type Kernel struct {")
	if start < 0 {
		t.Fatal("Kernel struct not found")
	}
	end := strings.Index(body[start:], "\n}")
	if end < 0 {
		t.Fatal("Kernel struct end not found")
	}
	structBody := body[start : start+end]
	n := strings.Count(structBody, "sync.Mutex")
	once := strings.Count(structBody, "sync.Once")
	const wantMutex, wantOnce = 11, 4
	if n != wantMutex {
		t.Errorf("Kernel has %s mutexes, want %d; update the package lock-order comment and this count", strconv.Itoa(n), wantMutex)
	}
	if once != wantOnce {
		t.Errorf("Kernel has %s sync.Once, want %d", strconv.Itoa(once), wantOnce)
	}
}
