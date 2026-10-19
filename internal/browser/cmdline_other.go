//go:build !linux && !darwin

package browser

import "errors"

// processCmdline has no implementation here, so chromeOwned treats every
// recorded pid as foreign and only clears the stale session.
func processCmdline(int) ([]byte, error) {
	return nil, errors.New("browser: command line is not readable on this platform")
}
