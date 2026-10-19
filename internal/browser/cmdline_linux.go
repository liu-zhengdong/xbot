package browser

import (
	"fmt"
	"os"
)

func processCmdline(pid int) ([]byte, error) {
	return os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
}
