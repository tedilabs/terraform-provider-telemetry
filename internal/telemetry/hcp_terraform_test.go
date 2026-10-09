package telemetry

import (
	"context"
	"reflect"
	"testing"
)

func TestHCPTerraformMetadata(t *testing.T) {
	env := map[string]string{
		"TFC_RUN_ID": "run-123", "TFC_WORKSPACE_ID": "ws-123", "TFC_WORKSPACE_NAME": "prod",
		"TFC_WORKSPACE_SLUG": "acme/prod", "TFC_PROJECT_ID": "prj-123", "TFC_PROJECT_NAME": "Default Project",
		"TFC_CONFIGURATION_VERSION_GIT_BRANCH": "main", "TFC_CONFIGURATION_VERSION_GIT_COMMIT_SHA": "abc123",
		"TFC_CONFIGURATION_VERSION_GIT_TAG": "", "TFC_UNRELATED": "ignored",
	}
	c := NewCollector()
	c.Getenv = func(key string) string { return env[key] }
	want := map[string]any{
		"run_id": "run-123", "workspace_id": "ws-123", "workspace_name": "prod", "workspace_slug": "acme/prod",
		"project_id": "prj-123", "project_name": "Default Project", "git_branch": "main", "git_commit": "abc123",
	}
	if got := c.Collect(context.Background(), Options{HCPTerraform: true}, false)["hcp_terraform"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	env = map[string]string{"TFC_WORKSPACE_NAME": "prod"}
	if got, ok := c.Collect(context.Background(), Options{HCPTerraform: true}, false)["hcp_terraform"]; ok {
		t.Fatalf("collected outside an HCP Terraform run: %v", got)
	}
}
