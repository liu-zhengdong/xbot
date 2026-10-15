// Package evals holds the proactivity suite that ships inside the xbot binary.
package evals

import "embed"

// Files is the default suite. evals/proactivity/*.yaml plus fixtures.
//
//go:embed proactivity
var Files embed.FS
