package telemetry

import "golang.org/x/sys/unix"

func hostInfo() (string, string, uint64) {
	version, _ := unix.Sysctl("kern.osproductversion")
	memory, _ := unix.SysctlUint64("hw.memsize")
	return "macOS", version, memory / (1024 * 1024)
}
