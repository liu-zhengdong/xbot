package vault

import (
	"errors"
	"fmt"
	"io"
)

const LocalSudo = "LOCAL_SUDO"

// WriteLocalSudo is the dedicated local askpass egress. It deliberately has no
// arbitrary-name argument and is not part of the daemon Backend interface.
func WriteLocalSudo(dir, keyPath string, out io.Writer) error {
	s, err := Open(dir, keyPath)
	if err != nil {
		return errors.New("sudo askpass: cannot open local vault")
	}
	sec, ok := s.Lookup(LocalSudo)
	if !ok || sec.Value == "" {
		return errors.New("sudo askpass: LOCAL_SUDO is not configured")
	}
	_, err = fmt.Fprintln(out, sec.Value)
	return err
}
