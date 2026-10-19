//go:build !race

package web

// raceEnabled is false in a normal build; see race_on_test.go.
const raceEnabled = false
