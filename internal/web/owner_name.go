package web

import (
	"regexp"
	"strings"

	"xbot/internal/person"
)

// windowsSID matches a person id made from a Windows SID ("u" + "S-1-5-21-…").
var windowsSID = regexp.MustCompile(`^uS-\d+(-\d+)+$`)

// ownerName is the name the window shows for the signed-in person.
// The person id is a storage key: on Windows it carries the account SID (#210).
// It returns the person's display name without a "DOMAIN\" prefix,
// then the id unless that is a SID, else "" (the page shows 我).
func (s *Server) ownerName() string {
	if p, err := person.Load(s.Root, s.owner); err == nil {
		name := strings.TrimSpace(p.DisplayName)
		if i := strings.LastIndex(name, `\`); i >= 0 {
			name = strings.TrimSpace(name[i+1:])
		}
		if name != "" && !windowsSID.MatchString("u"+name) {
			return name
		}
	}
	if windowsSID.MatchString(s.owner) {
		return ""
	}
	return s.owner
}
