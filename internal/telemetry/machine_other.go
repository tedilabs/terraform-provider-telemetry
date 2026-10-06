//go:build !darwin && !linux && !windows

package telemetry

import "runtime"

func hostInfo() (string, string, uint64) { return runtime.GOOS, "", 0 }
