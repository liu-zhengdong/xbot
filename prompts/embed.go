// Package prompts is the embedded per-model overlay tree.
// A model file is prompts/models/<provider>/<model>.md.
// This package ships no model text. An empty lookup leaves the shared rules unchanged.
package prompts

import "embed"

// Files is the embedded tree. models/README.md is not an overlay.
//
//go:embed models
var Files embed.FS
