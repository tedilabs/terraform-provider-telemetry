package telemetry

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Options and Collector are independent of the destination and Terraform SDK.
type Options struct {
	Machine       bool `tfsdk:"machine"`
	Network       bool `tfsdk:"network"`
	Git           bool `tfsdk:"git"`
	GitHub        bool `tfsdk:"github"`
	GitHubActions bool `tfsdk:"github_actions"`
	HCPTerraform  bool `tfsdk:"hcp_terraform"`
	Terraform     bool `tfsdk:"terraform"`
	Toolchain     bool `tfsdk:"toolchain"`
}

type Collector struct {
	Run            func(context.Context, string, ...string) ([]byte, error)
	Getenv         func(string) string
	ReadFile       func(string) ([]byte, error)
	LookupPublicIP func(context.Context) string
	Parent         func() (Process, bool)
	cache          *collectionCache
}

func NewCollector() Collector {
	return Collector{
		Run: runCommand, Getenv: os.Getenv, ReadFile: os.ReadFile, LookupPublicIP: lookupPublicIP, Parent: parentProcess,
		cache: &collectionCache{},
	}
}

// Collect caches only the explicitly listed metadata groups when cache is enabled.
// A Collector created by NewCollector can be shared by concurrent function calls.
func (c Collector) Collect(ctx context.Context, opts Options, cache bool) map[string]any {
	properties := map[string]any{}
	collect := func(key string, enabled bool, entry *cachedCollection, read func() map[string]any) {
		if !enabled {
			return
		}
		var data map[string]any
		if cache && entry != nil {
			data = entry.get(read)
		} else {
			data = read()
		}
		if data != nil {
			properties[key] = data
		}
	}
	// This fixed list is the cache allowlist. New collectors are uncached unless
	// explicitly added here; arbitrary extra_data never enters this cache.
	entries := c.cache
	if entries == nil {
		entries = &collectionCache{}
	}
	collect("machine", opts.Machine, &entries.machine, machine)
	collect("network", opts.Network, &entries.network, func() map[string]any { return c.network(ctx) })
	collect("git", opts.Git, &entries.git, func() map[string]any { return c.git(ctx) })
	collect("github", opts.GitHub, &entries.github, func() map[string]any { return c.github(ctx) })
	collect("github_actions", opts.GitHubActions, &entries.githubActions, c.actions)
	collect("hcp_terraform", opts.HCPTerraform, &entries.hcpTerraform, c.hcpTerraform)
	collect("terraform", opts.Terraform, &entries.terraform, func() map[string]any { return c.terraform(ctx) })
	collect("toolchain", opts.Toolchain, &entries.toolchain, func() map[string]any { return c.toolchain(ctx) })
	return properties
}

func (c Collector) actions() map[string]any {
	if c.Getenv("GITHUB_ACTIONS") != "true" {
		return nil
	}
	return c.githubActions()
}

func (c Collector) network(ctx context.Context) map[string]any {
	result := map[string]any{}
	if hostname, err := os.Hostname(); err == nil {
		result["hostname"] = hostname
	}
	if ip := c.LookupPublicIP(ctx); ip != "" {
		result["public_ip"] = ip
	}
	return result
}

func (c Collector) git(ctx context.Context) map[string]any {
	root, err := c.Run(ctx, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	result := map[string]any{"name": filepath.Base(strings.TrimSpace(string(root)))}
	for key, args := range map[string][]string{
		"branch": {"rev-parse", "--abbrev-ref", "HEAD"},
		"commit": {"rev-parse", "HEAD"},
		"remote": {"config", "--get", "remote.origin.url"},
	} {
		out, err := c.Run(ctx, "git", args...)
		if err != nil {
			continue
		}
		value := strings.TrimSpace(string(out))
		if key == "remote" {
			value = sanitizeRemote(value)
		}
		// `--abbrev-ref` prints HEAD for a detached HEAD, which has no current branch.
		if key == "branch" && value == "HEAD" {
			continue
		}
		if value != "" {
			result[key] = value
		}
	}
	return result
}

func sanitizeRemote(remote string) string {
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil || u.Host == "" {
			return ""
		}
		u.User, u.RawQuery, u.Fragment = nil, "", ""
		return u.String()
	}
	// SCP-style SSH remotes, e.g. git@github.com:owner/repository.git. Like Git, treat a colon
	// after a slash as part of a local path. A single letter before it is a Windows drive.
	// Do not transmit local filesystem remotes.
	host, path, ok := strings.Cut(remote, ":")
	if !ok || strings.ContainsAny(host, `/\`) || strings.ContainsAny(remote, "?#") {
		return ""
	}
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}
	if len(host) < 2 {
		return ""
	}
	return host + ":" + path
}

func (c Collector) github(ctx context.Context) map[string]any {
	out, err := c.Run(ctx, "gh", "api", "user", "--jq", "{login, id, name, html_url, account_type: .type}")
	if err != nil {
		return nil
	}
	var user map[string]any
	if json.Unmarshal(out, &user) != nil {
		return nil
	}
	result := map[string]any{}
	for _, key := range []string{"login", "id", "name", "html_url", "account_type"} {
		if value, ok := user[key]; ok && value != nil {
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (c Collector) githubActions() map[string]any {
	result := map[string]any{}
	for key, env := range map[string]string{
		"workflow": "GITHUB_WORKFLOW", "workflow_ref": "GITHUB_WORKFLOW_REF", "workflow_sha": "GITHUB_WORKFLOW_SHA",
		"job": "GITHUB_JOB", "run_id": "GITHUB_RUN_ID", "run_number": "GITHUB_RUN_NUMBER", "run_attempt": "GITHUB_RUN_ATTEMPT",
		"repository": "GITHUB_REPOSITORY", "repository_id": "GITHUB_REPOSITORY_ID", "repository_owner": "GITHUB_REPOSITORY_OWNER",
		"actor": "GITHUB_ACTOR", "actor_id": "GITHUB_ACTOR_ID", "triggering_actor": "GITHUB_TRIGGERING_ACTOR",
		"event_name": "GITHUB_EVENT_NAME", "ref": "GITHUB_REF", "sha": "GITHUB_SHA",
		"head_ref": "GITHUB_HEAD_REF", "base_ref": "GITHUB_BASE_REF", "server_url": "GITHUB_SERVER_URL",
		"runner_os": "RUNNER_OS", "runner_arch": "RUNNER_ARCH", "runner_environment": "RUNNER_ENVIRONMENT",
	} {
		if value := c.Getenv(env); value != "" {
			result[key] = value
		}
	}
	if server, repo, run := c.Getenv("GITHUB_SERVER_URL"), c.Getenv("GITHUB_REPOSITORY"), c.Getenv("GITHUB_RUN_ID"); server != "" && repo != "" && run != "" {
		result["run_url"] = strings.TrimRight(server, "/") + "/" + repo + "/actions/runs/" + run
	}
	return result
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0", "CHECKPOINT_DISABLE=1")
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Output()
}
