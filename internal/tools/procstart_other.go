//go:build unix && !linux && !darwin

package tools

// processStartTicks is unknown here, so a restarted process marks every
// running job lost instead of adopting a pid it cannot tell apart.
func processStartTicks(int) int64 { return 0 }
