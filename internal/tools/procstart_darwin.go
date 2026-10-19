package tools

import "golang.org/x/sys/unix"

// processStartTicks is pid's start time in microseconds since the epoch, from
// kern.proc.pid. It is only compared for equality with a value read the same
// way, so a reused pid is not the same process. Zero means unknown.
func processStartTicks(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_pid != int32(pid) {
		return 0
	}
	tv := kp.Proc.P_starttime
	return int64(tv.Sec)*1e6 + int64(tv.Usec)
}
