package telemetry

import (
	"fmt"
	"os"
	"strings"
)

func processStart(pid int) (string, bool) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	start, ok := parseProcStatStart(string(stat))
	if !ok {
		return "", false
	}
	// Start times count clock ticks since boot, so qualify them with the boot ID.
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(boot)) + "/" + start, true
}

// The command name can contain spaces and parentheses, so count the fields
// after its closing parenthesis. The start time is field 22 of the line.
func parseProcStatStart(stat string) (string, bool) {
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return "", false
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 20 {
		return "", false
	}
	return fields[19], true
}

func processExecutable(pid int) (string, bool) {
	path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	return path, err == nil && path != ""
}
