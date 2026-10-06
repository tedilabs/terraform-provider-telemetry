package telemetry

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func hostInfo() (string, string, uint64) {
	v := windows.RtlGetVersion()
	version := fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	// MEMORYSTATUSEX uses fixed-width fields on both 32-bit and 64-bit Windows.
	var status struct {
		Length, MemoryLoad                                                                                   uint32
		TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, TotalVirtual, AvailVirtual, AvailExtendedVirtual uint64
	}
	status.Length = uint32(unsafe.Sizeof(status))
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	if ok, _, _ := proc.Call(uintptr(unsafe.Pointer(&status))); ok == 0 {
		return "Windows", version, 0
	}
	return "Windows", version, status.TotalPhys / (1024 * 1024)
}
