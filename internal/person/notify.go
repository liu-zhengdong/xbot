package person

import "sync"

var (
	changeMu sync.Mutex
	changeFn func(string)
)

// OnChange registers the function called after a local identity write.
// A merge does not call it. A nil function clears the hook.
func OnChange(fn func(root string)) {
	changeMu.Lock()
	changeFn = fn
	changeMu.Unlock()
}

func notifyChange(root string) {
	changeMu.Lock()
	fn := changeFn
	changeMu.Unlock()
	if fn != nil {
		fn(root)
	}
}
