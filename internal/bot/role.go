package bot

import "time"

// Role picks a set of working defaults. chat is interactive: short turns and a
// small step cap. dev is engineering work: longer turns and more steps.
const (
	RoleChat = "chat"
	RoleDev  = "dev"
)

// RoleDefaults is the working profile one role implies.
type RoleDefaults struct {
	MaxSteps           int
	ForegroundMaxTools int
	ForegroundMaxTime  time.Duration
	// BackgroundMaxTime caps the wall-clock time one background task may spend
	// across all its segments. Engineering work gets a longer budget.
	BackgroundMaxTime time.Duration
}

// DefaultsForRole returns the working defaults for a role. An empty or unknown
// role is chat, the interactive default.
func DefaultsForRole(role string) RoleDefaults {
	if role == RoleDev {
		return RoleDefaults{MaxSteps: 24, ForegroundMaxTools: 8, ForegroundMaxTime: 60 * time.Second, BackgroundMaxTime: 45 * time.Minute}
	}
	return RoleDefaults{MaxSteps: 8, ForegroundMaxTools: 4, ForegroundMaxTime: 20 * time.Second, BackgroundMaxTime: 20 * time.Minute}
}

// Working resolves the role defaults together with any explicit overrides. A
// zero override keeps the role default; a negative override turns that limit
// off.
func (c Config) Working() RoleDefaults {
	d := DefaultsForRole(c.Role)
	if c.MaxSteps != 0 {
		d.MaxSteps = c.MaxSteps
	}
	if c.ForegroundMaxTools != 0 {
		d.ForegroundMaxTools = c.ForegroundMaxTools
	}
	if c.ForegroundMaxTime != 0 {
		d.ForegroundMaxTime = time.Duration(c.ForegroundMaxTime) * time.Second
	}
	return d
}
