package telemetry

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
}

type Collector struct {
	Run    func(context.Context, string, ...string) ([]byte, error)
	Getenv func(string) string
	cache  *collectionCache
}

func NewCollector() Collector {
	return Collector{Run: runCommand, Getenv: os.Getenv, cache: &collectionCache{}}
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
	collect("network", opts.Network, &entries.network, network)
	collect("git", opts.Git, &entries.git, func() map[string]any { return c.git(ctx) })
	collect("github", opts.GitHub, &entries.github, func() map[string]any { return c.github(ctx) })
	collect("github_actions", opts.GitHubActions, &entries.githubActions, c.actions)
	return properties
}

func machine() map[string]any {
	return map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "cpu_count": runtime.NumCPU()}
}

func (c Collector) actions() map[string]any {
	if c.Getenv("GITHUB_ACTIONS") != "true" {
		return nil
	}
	return c.githubActions()
}

func network() map[string]any {
	result := map[string]any{}
	if hostname, err := os.Hostname(); err == nil {
		result["hostname"] = hostname
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return result
	}
	ips := []string{}
	seen := map[string]bool{}
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || ip.IsLoopback() || ip.IsUnspecified() || seen[ip.String()] {
			continue
		}
		seen[ip.String()] = true
		ips = append(ips, ip.String())
	}
	sort.Strings(ips)
	result["ips"] = ips
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
	// SCP-style SSH remotes, e.g. git@github.com:owner/repository.git.
	if at := strings.IndexByte(remote, '@'); at >= 0 {
		remote = remote[at+1:]
	}
	if strings.Contains(remote, ":") && !strings.ContainsAny(remote, "?#") {
		return remote
	}
	// Do not transmit local filesystem remotes.
	return ""
}

func (c Collector) github(ctx context.Context) map[string]any {
	out, err := c.Run(ctx, "gh", "api", "user", "--jq", "{login, id, name, html_url}")
	if err != nil {
		return nil
	}
	var user map[string]any
	if json.Unmarshal(out, &user) != nil {
		return nil
	}
	result := map[string]any{}
	for _, key := range []string{"login", "id", "name", "html_url"} {
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
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Output()
}
