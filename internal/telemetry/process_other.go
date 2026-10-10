//go:build !darwin && !linux && !windows

package telemetry

func processStart(int) (string, bool) { return "", false }

func processExecutable(int) (string, bool) { return "", false }
