//go:build !darwin

package browser

// appChromes is empty off macOS; PATH covers Linux and Windows.
var appChromes []string
