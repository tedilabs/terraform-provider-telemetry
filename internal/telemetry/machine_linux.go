package telemetry

import (
	"os"

	"golang.org/x/sys/unix"
)

func hostInfo() (string, string, uint64) {
	name, version := "Linux", ""
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		data, _ = os.ReadFile("/usr/lib/os-release")
	}
	if n, v := parseOSRelease(data); n != "" {
		name, version = n, v
	}
	var info unix.Sysinfo_t
	var memory uint64
	if unix.Sysinfo(&info) == nil {
		memory = uint64(info.Totalram) * uint64(info.Unit) / (1024 * 1024)
	}
	return name, version, memory
}
