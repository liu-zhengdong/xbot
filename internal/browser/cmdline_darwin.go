package browser

import (
	"bytes"
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// processCmdline reads kern.procargs2, which macOS lays out as a 4-byte argc,
// the executable path, NUL padding, then argc NUL-terminated arguments and the
// environment. Only the arguments are returned, in /proc/<pid>/cmdline form, so
// a profile path that appears only in the environment does not count.
func processCmdline(pid int) ([]byte, error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, err
	}
	return parseProcargs2(raw)
}

func parseProcargs2(raw []byte) ([]byte, error) {
	if len(raw) < 4 {
		return nil, errors.New("browser: short procargs")
	}
	argc := int(binary.NativeEndian.Uint32(raw))
	rest := raw[4:]
	// Skip the executable path and the NULs that pad it.
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return nil, errors.New("browser: malformed procargs")
	}
	rest = rest[i:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	end := 0
	for n := 0; n < argc; n++ {
		j := bytes.IndexByte(rest[end:], 0)
		if j < 0 {
			end = len(rest)
			break
		}
		end += j + 1
	}
	return rest[:end], nil
}
