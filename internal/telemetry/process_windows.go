package telemetry

import (
	"strconv"

	"golang.org/x/sys/windows"
)

func processStart(pid int) (string, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(handle)
	var creation, exit, kernel, user windows.Filetime
	if windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user) != nil {
		return "", false
	}
	return strconv.FormatInt(creation.Nanoseconds(), 10), true
}

func processExecutable(pid int) (string, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(handle)
	path := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(path))
	if windows.QueryFullProcessImageName(handle, 0, &path[0], &size) != nil {
		return "", false
	}
	return windows.UTF16ToString(path[:size]), true
}
