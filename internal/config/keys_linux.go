package config

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func scrubEnviron(name, value string) {
	if name == "" || value == "" {
		return
	}
	start, end, err := stackSpan()
	if err != nil || end <= start {
		return
	}
	f, err := os.OpenFile("/proc/self/mem", os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer f.Close()
	buf := make([]byte, end-start)
	n, err := syscall.Pread(int(f.Fd()), buf, int64(start))
	if n <= 0 || (err != nil && n == 0) {
		return
	}
	buf = buf[:n]
	needle := []byte(name + "=" + value)
	blank := bytes.Repeat([]byte{0}, len(value))
	for {
		i := bytes.Index(buf, needle)
		if i < 0 {
			return
		}
		at := i + len(name) + 1
		_, _ = syscall.Pwrite(int(f.Fd()), blank, int64(start)+int64(at))
		copy(buf[at:i+len(needle)], blank)
	}
}

func stackSpan() (uint64, uint64, error) {
	raw, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasSuffix(strings.TrimSpace(line), "[stack]") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			break
		}
		rng := strings.Split(fields[0], "-")
		if len(rng) != 2 {
			break
		}
		start, err1 := strconv.ParseUint(rng[0], 16, 64)
		end, err2 := strconv.ParseUint(rng[1], 16, 64)
		if err1 != nil || err2 != nil {
			return 0, 0, err1
		}
		return start, end, nil
	}
	return 0, 0, os.ErrNotExist
}
