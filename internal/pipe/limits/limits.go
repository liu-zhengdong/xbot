// Package limits shares the wire body budget with API handlers without
// coupling the web server to the pipe transport.
package limits

const MaxBody = 700 << 10
