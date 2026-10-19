package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"xbot/internal/version"
)

// The states an Updater reports. A page shows them as they are.
const (
	StateIdle        = "idle"        // nothing newer is known
	StateChecking    = "checking"    // a check is under way
	StateDownloading = "downloading" // a newer release is being downloaded
	StateReady       = "ready"       // downloaded and checked; a restart installs it
	StateError       = "error"       // the last check or download failed
	StateManual      = "manual"      // this platform installs with its own installer
)

// DefaultInterval is how often the release feed is asked.
const DefaultInterval = 6 * time.Hour

// FirstDelay lets the process settle before the first check.
const FirstDelay = 30 * time.Second

// Status is what an Updater currently knows, as a page shows it.
type Status struct {
	State   string `json:"state"`
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	Notes   string `json:"notes,omitempty"`
	URL     string `json:"url,omitempty"`
	// Download is the file a platform that installs with its own installer
	// offers instead, when the release has one.
	Download string `json:"download,omitempty"`
	Error    string `json:"error,omitempty"`
	Done     int64  `json:"done,omitempty"`
	Total    int64  `json:"total,omitempty"`
}

// Updater checks for a newer release, downloads it in the background, and
// installs it when the owner asks. Every method is safe for concurrent use.
type Updater struct {
	root string
	// Relaunch starts the process again. nil when nothing can: the caller
	// then only installs, and the new version runs at the next start.
	Relaunch func()
	// Interval and FirstDelay default to DefaultInterval and FirstDelay.
	Interval   time.Duration
	FirstDelay time.Duration
	// Now and notify are replaced in tests.
	now func() time.Time

	mu     sync.Mutex
	state  string
	info   Info
	err    string
	done   int64
	total  int64
	staged string
	// installing keeps a second click, or a check, from starting while a
	// swap is under way.
	installing bool
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewUpdater builds the updater for one data root and restores a package a
// previous run downloaded.
func NewUpdater(root string) *Updater {
	u := &Updater{root: root, state: StateIdle, now: time.Now}
	if v := Pending(root); v != "" {
		if cmp, err := Compare(v, version.Version); err == nil && cmp > 0 {
			u.state = StateReady
			u.info = Info{Current: version.Version, Latest: v, Available: true}
			u.staged = StagedPath(root, v)
		} else {
			// An older or unreadable package is no use: a version released
			// since is the one to install.
			ClearStaged(root)
		}
	}
	return u
}

// Status is what the updater knows now.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := Status{
		State:   u.state,
		Current: version.Version,
		Latest:  u.info.Latest,
		Notes:   u.info.Notes,
		URL:     u.info.URL,
		Error:   u.err,
		Done:    u.done,
		Total:   u.total,
	}
	if s.State == StateManual && s.Latest != "" {
		s.Download = u.info.AssetURL(DownloadAsset(u.info.Latest))
	}
	return s
}

// Run checks now and then until ctx ends. It downloads a newer release as
// soon as it finds one, so the owner's later click has nothing to wait for.
func (u *Updater) Run(ctx context.Context) {
	u.mu.Lock()
	u.ctx, u.cancel = context.WithCancel(ctx)
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.cancel = nil
		u.mu.Unlock()
	}()
	first := u.FirstDelay
	if first == 0 {
		first = FirstDelay
	}
	every := u.Interval
	if every == 0 {
		every = DefaultInterval
	}
	timer := time.NewTimer(first)
	defer timer.Stop()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-tick.C:
		}
		if err := u.Check(ctx); err != nil {
			// A failed check is not worth stopping for: the next one
			// tries again, and the page shows why this one did.
			continue
		}
	}
}

// Check asks the feed once. It downloads a newer release unless one is already
// downloaded.
func (u *Updater) Check(ctx context.Context) error {
	if !u.begin(StateChecking) {
		return nil
	}
	info, err := Check(ctx, version.Version)
	if err != nil {
		u.fail(err)
		return err
	}
	if !info.Available {
		u.mu.Lock()
		u.state, u.info, u.err = StateIdle, info, ""
		u.mu.Unlock()
		return nil
	}
	if AssetName(info.Latest) == "" {
		// Windows installs with its own installer; xbot only offers it.
		u.mu.Lock()
		u.state, u.info, u.err = StateManual, info, ""
		u.mu.Unlock()
		return nil
	}
	if cmp, cerr := Compare(info.Latest, Pending(u.root)); cerr == nil && cmp <= 0 && u.staged != "" {
		u.mu.Lock()
		u.state, u.info = StateReady, info
		u.mu.Unlock()
		return nil
	}
	u.mu.Lock()
	u.state, u.info, u.err = StateDownloading, info, ""
	u.done, u.total = 0, info.AssetSize(AssetName(info.Latest))
	u.mu.Unlock()
	staged, err := Stage(ctx, info, u.root, func(done, total int64) {
		u.mu.Lock()
		u.done = done
		if total > 0 {
			u.total = total
		}
		u.mu.Unlock()
	})
	if err != nil {
		u.fail(err)
		return err
	}
	u.mu.Lock()
	u.state, u.staged, u.err = StateReady, staged, ""
	u.mu.Unlock()
	return nil
}

// begin claims the updater for one operation, unless one is under way.
func (u *Updater) begin(state string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.state == StateChecking || u.state == StateDownloading || u.installing {
		return false
	}
	u.state, u.err = state, ""
	return true
}

// fail records why the last attempt failed. An update already downloaded
// stays instalable: the page keeps its button, and the reason is only shown on
// the next check.
func (u *Updater) fail(err error) {
	u.mu.Lock()
	if u.staged == "" {
		u.state = StateError
	} else {
		u.state = StateReady
	}
	u.err = err.Error()
	u.mu.Unlock()
}

// ErrNothingStaged means Install was asked for with nothing downloaded.
var ErrNothingStaged = fmt.Errorf("nothing downloaded to install")

// Install puts the downloaded version in place and starts the process again.
// A failure leaves the running version untouched.
func (u *Updater) Install() error {
	u.mu.Lock()
	if u.installing {
		u.mu.Unlock()
		return errors.New("正在安装，请等它重启")
	}
	if u.state == StateDownloading || u.state == StateChecking {
		u.mu.Unlock()
		return errors.New("正在下载新版本，稍等一下")
	}
	u.installing = true
	staged := u.staged
	latest := u.info.Latest
	relaunch := u.Relaunch
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.installing = false
		u.mu.Unlock()
	}()
	if staged == "" || latest == "" {
		return ErrNothingStaged
	}
	if _, err := os.Stat(staged); err != nil {
		u.mu.Lock()
		u.state, u.staged = StateError, ""
		u.err = "the downloaded update is gone; check again"
		u.mu.Unlock()
		return ErrNothingStaged
	}
	if err := Install(staged); err != nil {
		u.fail(err)
		return err
	}
	ClearStaged(u.root)
	u.mu.Lock()
	u.state, u.staged = StateIdle, ""
	u.mu.Unlock()
	// The process ends here when a relaunch is wired up: syscall.Exec
	// replaces this image, and a window restart quits the app.
	if relaunch != nil {
		relaunch()
	}
	return nil
}

// Stop cancels a download under way.
func (u *Updater) Stop() {
	u.mu.Lock()
	cancel := u.cancel
	u.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
