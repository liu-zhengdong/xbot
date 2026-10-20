package appshell

import (
	"errors"
	"time"

	"github.com/egoist/mygo"
)

// NotifyAway shows a native notification only when our window is not in front,
// so a background event (such as a pairing request) is not announced to someone
// already looking at the app; clicking it brings the window forward.
func NotifyAway(title, body string) (shown bool, err error) {
	if !running.Load() {
		return false, errors.New("appshell: application is not ready")
	}
	if !mygo.NotificationsSupported() {
		return false, errors.New("appshell: native notifications unavailable")
	}
	if mygo.FocusedWindow() != nil {
		return false, nil
	}
	n := mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body})
	n.OnClick(bringForward)
	return true, n.Show()
}

// OpenPath opens a file or directory with the default application. It is only
// available once the app loop is running, like native notifications.
func OpenPath(path string) error {
	if !running.Load() {
		return errors.New("appshell: application is not ready")
	}
	return mygo.Shell.OpenPath(path)
}

// bringForward restores, shows and focuses our window. Windows refuses
// SetForegroundWindow from a process the user is not interacting with, so keep
// asking for a moment after the click, without taking the main loop thread.
func bringForward() {
	go func() {
		for range 10 {
			if mygo.FocusedWindow() != nil {
				return // In front: stop, so switching away again is not undone.
			}
			for _, w := range mygo.Windows() {
				if w.IsMinimized() {
					w.Restore()
				}
				w.Show()
				w.Focus()
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}
