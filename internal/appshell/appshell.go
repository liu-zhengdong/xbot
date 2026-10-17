// Package appshell contains the native window framework. The page never calls
// shell APIs; swapping frameworks stays inside this package.
package appshell

import (
	"errors"
	"net/url"
	"sync/atomic"

	"github.com/egoist/mygo"
)

type Options struct {
	Title, URL    string
	StatePath     string
	Width, Height int
}

func defaults(opts Options) Options {
	if opts.Width <= 0 {
		opts.Width = 1100
	}
	if opts.Height <= 0 {
		opts.Height = 720
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
		if opts.StatePath != "" {
			// MyGo restores the bounds only when they land on a connected
			// display, and saves them when the window closes or the app quits.
			mygo.App.SetWindowStateStore(fileStore{opts.StatePath})
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: opts.Title, URL: opts.URL, Width: opts.Width, Height: opts.Height,
			StateKey: stateKey, MinWidth: 900, MinHeight: 560, BackgroundColor: "#ffffff",
			TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 40,
			TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
		})
		win.Page().OnPageTitleUpdated(func(e *mygo.TitleEvent) { e.PreventDefault() })
		origin, _ := url.Parse(opts.URL)
		win.Page().OnWillNavigate(func(e *mygo.NavigateEvent) {
			target, err := url.Parse(e.URL)
			if err == nil && origin != nil && target.Scheme == origin.Scheme && target.Host == origin.Host && target.User == nil {
				return
			}
			e.PreventDefault()
			if err == nil && (target.Scheme == "http" || target.Scheme == "https" || target.Scheme == "mailto") {
				go mygo.Shell.OpenExternal(e.URL)
			}
		})
		win.OnClosed(func() { mygo.App.Quit() })
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
