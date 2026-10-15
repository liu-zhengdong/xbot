// Package evals holds the suites that ship inside the xbot binary.
package evals

import "embed"

// Files is the embedded suites: proactivity and proactivity-holdout.
//
//go:embed proactivity proactivity-holdout
var Files embed.FS
