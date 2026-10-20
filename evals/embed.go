// Package evals holds the suites that ship inside the xbot binary.
package evals

import "embed"

// Files is the embedded suites: proactivity, proactivity-holdout, and
// dispatch.
//
//go:embed proactivity proactivity-holdout dispatch
var Files embed.FS
