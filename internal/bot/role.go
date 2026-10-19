package bot

import "time"

// Role picks a set of working defaults. chat is interactive: short turns and a
// small foreground budget. dev is engineering work: longer turns.
const (
	RoleChat = "chat"
	RoleDev  = "dev"
)

// RoleDefaults is the working profile one role implies.
type RoleDefaults struct {
	ForegroundMaxTools int
	ForegroundMaxTime  time.Duration
	// BackgroundMaxTime caps the wall-clock time one background task may spend.
	// It is what actually bounds a background: engineering work gets longer.
	BackgroundMaxTime time.Duration
}

// DefaultsForRole returns the working defaults for a role. An empty or unknown
// role is chat, the interactive default.
func DefaultsForRole(role string) RoleDefaults {
	if role == RoleDev {
		return RoleDefaults{ForegroundMaxTools: 8, ForegroundMaxTime: 60 * time.Second, BackgroundMaxTime: 2 * time.Hour}
	}
	return RoleDefaults{ForegroundMaxTools: 4, ForegroundMaxTime: 20 * time.Second, BackgroundMaxTime: 45 * time.Minute}
}

// Working resolves the role defaults together with any explicit overrides. A
// zero override keeps the role default; a negative override turns that limit
// off.
func (c Config) Working() RoleDefaults {
	d := DefaultsForRole(c.Role)
	if c.ForegroundMaxTools != 0 {
		d.ForegroundMaxTools = c.ForegroundMaxTools
	}
	if c.ForegroundMaxTime != 0 {
		d.ForegroundMaxTime = time.Duration(c.ForegroundMaxTime) * time.Second
	}
	if c.BackgroundMaxTime != 0 {
		d.BackgroundMaxTime = time.Duration(c.BackgroundMaxTime) * time.Second
	}
	return d
}
