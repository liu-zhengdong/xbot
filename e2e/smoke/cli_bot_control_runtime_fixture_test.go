//go:build smoke

package smoke

// Independent wire views fixed by f609f3ba / ADR0053.
type cliBotControlPinReply struct {
	Name         string
	Pinned       bool
	Applied      bool
	Stage, Error string
}
type cliBotControlPrimaryReply struct {
	Name, Primary string
	Applied       bool
	Stage, Error  string
}
