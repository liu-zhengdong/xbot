// Package version carries the build version stamped in at link time.
package version

// Version is the xbot build version. The release script overrides it:
//
//	go build -ldflags "-X xbot/internal/version.Version=v0.1.0"
var Version = "dev"
