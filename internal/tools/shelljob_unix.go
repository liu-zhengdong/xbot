//go:build unix

package tools

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func setGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 500 * time.Millisecond
}

// killProcess signals the command's process group. The child is the leader
// because setGroup set Setpgid, so children that did not leave the group die too.
func killProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

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

// sameProcess reports that pid is alive and still the process that started
// at start ticks. A zero start never matches.
func sameProcess(pid int, start int64) bool {
	if pid <= 0 || start <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	return processStartTicks(pid) == start
}
