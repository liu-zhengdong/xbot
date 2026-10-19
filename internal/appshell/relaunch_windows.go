//go:build windows

package appshell

import "errors"

// Relaunch on Windows: the installer replaces the running app, so an update
// there is the setup.exe the page offers rather than a restart of xbot
// itself.
func Relaunch(exe string, args []string) error {
	return errors.New("appshell: XBot updates itself with its installer on Windows")
}
