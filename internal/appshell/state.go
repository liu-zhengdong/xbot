package appshell

import (
	"encoding/json"
	"io"
	"os"

	"github.com/egoist/mygo"

	"xbot/internal/fsutil"
)

const stateKey = "main"

// fileStore keeps MyGo's window state in one small JSON file. Entries that
// are not a sane window size are dropped so a corrupt file never yields a
// tiny or giant window; MyGo itself ignores bounds off every display.
type fileStore struct{ path string }

func validState(s mygo.WindowState) bool {
	return s.Width >= 900 && s.Height >= 560 && s.Width <= 10000 && s.Height <= 10000
}

func (f fileStore) LoadWindowStates() (map[string]mygo.WindowState, error) {
	fh, err := os.Open(f.path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	data, err := io.ReadAll(io.LimitReader(fh, 4097))
	if err != nil {
		return nil, err
	}
	var states map[string]mygo.WindowState
	if len(data) > 4096 || json.Unmarshal(data, &states) != nil {
		return nil, nil
	}
	for k, s := range states {
		if k != stateKey || !validState(s) {
			delete(states, k)
		}
	}
	return states, nil
}

func (f fileStore) SaveWindowStates(states map[string]mygo.WindowState) error {
	keep := map[string]mygo.WindowState{}
	if s, ok := states[stateKey]; ok && validState(s) {
		keep[stateKey] = s
	}
	data, err := json.Marshal(keep)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(f.path, data, 0600)
}
