package telemetry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (c Collector) terraform() map[string]any {
	result := map[string]any{"in_automation": c.Getenv("TF_IN_AUTOMATION") != ""}
	if workspace := c.Getenv("TF_WORKSPACE"); workspace != "" {
		result["workspace"], result["workspace_source"] = workspace, "environment"
		return result
	}
	dir := c.Getenv("TF_DATA_DIR")
	if dir == "" {
		dir = ".terraform"
	}
	data, err := c.ReadFile(filepath.Join(dir, "environment"))
	if errors.Is(err, os.ErrNotExist) {
		result["workspace"], result["workspace_source"] = "default", "default"
	} else if err == nil {
		if workspace := strings.TrimSpace(string(data)); workspace != "" {
			result["workspace"], result["workspace_source"] = workspace, "data_directory"
		}
	}
	return result
}
