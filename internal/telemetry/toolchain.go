package telemetry

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

func (c Collector) toolchain(ctx context.Context) map[string]any {
	// Inspect available tools on PATH, not the identity of the invoking CLI.
	tools := []struct {
		key, command string
		args         []string
		version      string
	}{
		{key: "terraform", command: "terraform", args: []string{"version", "-json"}},
		{key: "opentofu", command: "tofu", args: []string{"version", "-json"}},
		{key: "git", command: "git", args: []string{"--version"}},
		{key: "github_cli", command: "gh", args: []string{"--version"}},
	}
	var wg sync.WaitGroup
	for i := range tools {
		wg.Go(func() {
			tool := &tools[i]
			out, err := c.Run(ctx, tool.command, tool.args...)
			if err != nil {
				return
			}
			if tool.command == "terraform" || tool.command == "tofu" {
				var data struct {
					Version string `json:"terraform_version"`
				}
				if json.Unmarshal(out, &data) == nil {
					tool.version = data.Version
				}
			} else {
				parts := strings.Fields(string(out))
				if len(parts) >= 3 && parts[0] == tool.command && parts[1] == "version" {
					tool.version = parts[2]
				}
			}
		})
	}
	wg.Wait()
	result := map[string]any{}
	for _, tool := range tools {
		if tool.version != "" {
			result[tool.key] = tool.version
		}
	}
	return result
}
