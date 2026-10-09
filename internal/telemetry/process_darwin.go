package telemetry

import (
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
