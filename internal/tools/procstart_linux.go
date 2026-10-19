package tools

import (
	"bytes"
	"os"
	"strconv"
	"strings"
)

// processStartTicks is the kernel starttime field for pid. A later process
// can compare it to the value stored in the job so a reused pid is not the
// same process. Zero means unknown.
func processStartTicks(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	rparen := bytes.LastIndexByte(b, ')')
	if rparen < 0 || rparen+2 >= len(b) {
		return 0
	}
	fields := strings.Fields(string(b[rparen+2:]))
	// Fields after comm: state is 0 here, starttime is field 22 of stat, index 19.
	if len(fields) < 20 {
		return 0
	}
	n, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0
	}
	return n
}
