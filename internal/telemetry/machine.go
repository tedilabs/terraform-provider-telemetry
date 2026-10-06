package telemetry

import (
	"runtime"
	"strconv"
	"strings"
)

func machine() map[string]any {
	name, version, memoryMiB := hostInfo()
	os := map[string]any{"name": name}
	if version != "" {
		os["version"] = version
	}
	result := map[string]any{"os": os, "arch": runtime.GOARCH, "cpu_count": runtime.NumCPU()}
	if memoryMiB > 0 {
		result["memory_size"] = memoryMiB
	}
	return result
}

// Read only the two allowlisted fields; never source os-release as shell code.
func parseOSRelease(data []byte) (name, version string) {
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || (key != "NAME" && key != "VERSION_ID") {
			continue
		}
		if strings.HasPrefix(value, `"`) {
			decoded, err := strconv.Unquote(value)
			if err != nil {
				continue
			}
			value = decoded
		} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			value = value[1 : len(value)-1]
		}
		if key == "NAME" {
			name = value
		} else {
			version = value
		}
	}
	return
}
