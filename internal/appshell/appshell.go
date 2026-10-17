// Package appshell contains the native window framework. The page never calls
// shell APIs; swapping frameworks stays inside this package.
package appshell

import (
	"errors"
	"sync/atomic"

	"github.com/egoist/mygo"
)

type Options struct {
	Title, URL    string
	Width, Height int
}

func defaults(opts Options) Options {
	if opts.Width <= 0 {
		opts.Width = 1100
	}
	if opts.Height <= 0 {
		opts.Height = 760
	}
	return opts
}

var running atomic.Bool

// Run opens one window when ready and blocks until all windows close or Quit.
// It must be called on the main goroutine (MyGo locks it to the main thread).
func Run(opts Options, ready func()) error {
	opts = defaults(opts)
	var windowErr error
	mygo.App.WhenReady(func() {
		if !mygo.WebViewAvailable() {
			windowErr = errors.New("appshell: system WebView unavailable")
			mygo.App.Quit()
			return
		}
		mygo.NewWindow(mygo.WindowOptions{Title: opts.Title, URL: opts.URL, Width: opts.Width, Height: opts.Height})
		running.Store(true)
		if ready != nil {
			ready()
		}
	})
	defer running.Store(false)
	if err := mygo.App.Run(); err != nil {
		return err
	}
	return windowErr
}

func Quit() { mygo.App.Quit() }

// Notify shows a native notification while the shell is running.
func Notify(title, body string) error {
	if !running.Load() {
		return errors.New("appshell: application is not ready")
	}
	if !mygo.NotificationsSupported() {
		return errors.New("appshell: native notifications unavailable")
	}
	return mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body}).Show()
}
