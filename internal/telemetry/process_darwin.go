package telemetry

import (
	"bytes"
	"fmt"

	"golang.org/x/sys/unix"
)

func processStart(pid int) (string, bool) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || int(info.Proc.P_pid) != pid {
		return "", false
	}
	start := info.Proc.P_starttime
	if start.Sec == 0 {
		return "", false
	}
	return fmt.Sprintf("%d.%06d", start.Sec, start.Usec), true
}

func processExecutable(pid int) (string, bool) {
	args, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", false
	}
	return parseProcArgs(args)
}

// kern.procargs2 starts with the 32-bit argument count, followed by the
// null-terminated executable path.
func parseProcArgs(data []byte) (string, bool) {
	if len(data) < 5 {
		return "", false
	}
	path := data[4:]
	if end := bytes.IndexByte(path, 0); end >= 0 {
		path = path[:end]
	}
	return string(path), len(path) > 0
}
