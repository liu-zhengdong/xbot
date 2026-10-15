package link

import (
	"os/user"

	"xbot/internal/tools"
)

// Terminal is the local chat link. It has no outbound tools.
// User lines become Events; the kernel's assistant text is the reply.
type Terminal struct{}

func (Terminal) Name() string { return NameTerminal }

func (Terminal) Tools() []tools.Tool { return nil }

// Account maps the OS user to an external id on this link.
// person.Identify turns that id into a Person. Later links do the same for their accounts.
func Account() (externalID, display string, err error) {
	u, err := user.Current()
	if err != nil {
		return "", "", err
	}
	return "uid:" + u.Uid, u.Username, nil
}
