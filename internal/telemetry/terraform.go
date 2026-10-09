package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (c Collector) terraform(ctx context.Context) map[string]any {
	result := map[string]any{}
	if c.Parent != nil {
		if parent, ok := c.Parent(); ok {
			if parent.Start != "" {
				result["command_id"] = commandID(parent)
			}
			// Run only a recognized Terraform or OpenTofu executable.
			if cli, ok := cliName(parent.Executable); ok {
				result["cli"] = cli
				if out, err := c.Run(ctx, parent.Executable, "version", "-json"); err == nil {
					if version := terraformVersion(out); version != "" {
						result["cli_version"] = version
					}
				}
			}
		}
	}
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

// cliName recognizes the executable of the CLI that started this provider.
func cliName(executable string) (string, bool) {
	name := strings.ToLower(executable[strings.LastIndexAny(executable, `/\`)+1:])
	switch strings.TrimSuffix(name, ".exe") {
	case "terraform":
		return "terraform", true
	case "tofu":
		return "opentofu", true
	}
	return "", false
}
