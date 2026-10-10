package telemetry

// hcpTerraform reads the run environment variables that HCP Terraform sets for each run.
func (c Collector) hcpTerraform() map[string]any {
	if c.Getenv("TFC_RUN_ID") == "" {
		return nil
	}
	result := map[string]any{}
	for key, env := range map[string]string{
		"run_id": "TFC_RUN_ID", "workspace_id": "TFC_WORKSPACE_ID", "workspace_name": "TFC_WORKSPACE_NAME",
		"workspace_slug": "TFC_WORKSPACE_SLUG", "project_id": "TFC_PROJECT_ID", "project_name": "TFC_PROJECT_NAME",
		"git_branch": "TFC_CONFIGURATION_VERSION_GIT_BRANCH", "git_commit": "TFC_CONFIGURATION_VERSION_GIT_COMMIT_SHA",
		"git_tag": "TFC_CONFIGURATION_VERSION_GIT_TAG",
	} {
		if value := c.Getenv(env); value != "" {
			result[key] = value
		}
	}
	return result
}
