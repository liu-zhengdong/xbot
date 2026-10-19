//go:build race

package web

// raceEnabled reports that this binary was built with -race. The detector
// instruments every memory access, which multiplies the cost of a hot path by
// roughly 3-10x, so a wall-clock latency target measured under it says nothing
// about production.
const raceEnabled = true
