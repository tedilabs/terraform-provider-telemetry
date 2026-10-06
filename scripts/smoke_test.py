#!/usr/bin/env python3
"""Build and exercise real Terraform against a loopback-only PostHog stub."""

import http.server
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading

ROOT = Path(__file__).resolve().parents[1]


def main():
    events = []

    class Handler(http.server.BaseHTTPRequestHandler):
        status = 200

        def do_POST(self):
            assert self.path == "/i/v0/e/"
            events.append(json.loads(self.rfile.read(int(self.headers["Content-Length"]))))
            self.send_response(self.status)
            self.end_headers()
            self.wfile.write(b"{}")

        def log_message(self, *_):
            pass

    class Server(http.server.ThreadingHTTPServer):
        # Terraform can evaluate many checks concurrently. Avoid dropping local
        # test connections at the server's default small listen backlog.
        request_queue_size = 512

    server = Server(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        with tempfile.TemporaryDirectory(prefix="terraform-telemetry-smoke-") as directory:
            work = Path(directory)
            subprocess.run(["git", "-c", "init.defaultBranch=main", "init", "--quiet"], cwd=work, check=True)
            subprocess.run(["git", "-c", "user.name=Smoke Test", "-c", "user.email=smoke@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "--quiet", "--allow-empty", "-m", "Smoke test"], cwd=work, check=True)
            subprocess.run(["git", "remote", "add", "origin", "https://test:fake-token@example.invalid/demo/infra.git?token=fake"], cwd=work, check=True)
            binary_dir = work / "bin"
            binary_dir.mkdir()
            subprocess.run(["go", "build", "-o", str(binary_dir / "terraform-provider-telemetry"), "."], cwd=ROOT, check=True)
            config = work / "terraform.rc"
            config.write_text('provider_installation {\n dev_overrides {\n "registry.terraform.io/tedilabs/telemetry" = ' + json.dumps(str(binary_dir)) + '\n }\n direct {}\n}\n')
            (work / "main.tf").write_text('''
terraform {
  required_providers {
    telemetry = { source = "tedilabs/telemetry" }
  }
}
variable "connection" {
  type = object({ host = string, project_token = string })
  sensitive = true
}
check "capture" {
  assert {
    condition = provider::telemetry::capture_posthog(
      var.connection,
      { machine = true, network = false, git = true, github = false, github_actions = false },
      { workspace = terraform.workspace, count = 9007199254740993, nested = { enabled = true }, items = ["a", 2] }
    )
    error_message = "Telemetry must never fail."
  }
}
''')
            env = {k: v for k, v in os.environ.items() if not k.startswith(("TF_", "TF_LOG"))}
            env["TF_CLI_CONFIG_FILE"] = str(config)
            env["TF_IN_AUTOMATION"] = "1"
            env["CHECKPOINT_DISABLE"] = "1"
            env["TF_VAR_connection"] = json.dumps({"host": f"http://127.0.0.1:{server.server_port}", "project_token": "smoke-only"})

            def terraform(*args):
                result = subprocess.run(["terraform", *args, "-no-color"], cwd=work, env=env, text=True, capture_output=True)
                if result.returncode:
                    raise AssertionError(result.stdout + result.stderr)
                output = result.stdout + result.stderr
                assert "Check block assertion failed" not in output, output
                assert "Reading..." not in output, output
                return output

            terraform("validate")
            before = len(events)
            terraform("plan", "-input=false", "-out=saved.tfplan")
            assert len(events) > before, "plan did not capture"
            plan_count = len(events) - before
            before = len(events)
            # Saved plan must be last positional argument.
            result = subprocess.run(["terraform", "apply", "-input=false", "-no-color", "saved.tfplan"], cwd=work, env=env, text=True, capture_output=True)
            assert result.returncode == 0, result.stdout + result.stderr
            assert "Check block assertion failed" not in result.stdout + result.stderr
            assert len(events) > before, "saved-plan apply did not capture"
            apply_count = len(events) - before
            state = json.loads((work / "terraform.tfstate").read_text())
            assert state.get("resources", []) == [], state
            assert all(check["status"] == "pass" for check in state["check_results"])
            assert all(event["properties"]["extra_data"]["count"] == 9007199254740993 for event in events)
            assert all("network" not in event["properties"] and "github" not in event["properties"] for event in events)
            assert all(event["properties"]["machine"]["os"] for event in events)
            assert all(event["properties"]["git"]["name"] == work.name for event in events)
            assert all(event["properties"]["git"]["remote"] == "https://example.invalid/demo/infra.git" for event in events)
            assert all(event["properties"]["git"]["branch"] == "main" and event["properties"]["git"]["commit"] for event in events)

            Handler.status = 500
            before = len(events)
            terraform("apply", "-input=false", "-auto-approve")
            assert len(events) > before, "no-change apply did not attempt capture"
            # Exercise the optional options.cache_enabled boolean through the real CLI.
            source = work / "main.tf"
            source.write_text(source.read_text().replace('github_actions = false }', 'github_actions = false, cache_enabled = false }'))
            before = len(events)
            terraform("plan", "-input=false")
            assert len(events) > before, "options.cache_enabled=false did not capture"
            # Real module instances verify cross-function sharing and list decoding.
            Handler.status = 200
            module_dir = work / "module"
            module_dir.mkdir()
            header = source.read_text().split('check "capture"')[0]
            (module_dir / "main.tf").write_text(header + '''
variable "module_name" { type = string }
variable "instance_id" { type = string }
variable "deduplication_enabled" { type = bool }
variable "deduplication_keys" { type = list(string) }
check "capture" {
  assert {
    condition = provider::telemetry::capture_posthog(
      var.connection,
      {
        machine = true
        network = false
        git = false
        github = false
        github_actions = false
        deduplication_enabled = var.deduplication_enabled
        deduplication_keys = var.deduplication_keys
      },
      { module = var.module_name, instance = var.instance_id, workspace = terraform.workspace }
    )
    error_message = "Telemetry must never fail."
  }
}
''')
            source.write_text(header + '''
variable "deduplication_enabled" {
  type = bool
  default = true
}
variable "deduplication_keys" {
  type = list(string)
  default = ["extra_data.module", "extra_data.workspace"]
}
module "counted" {
  source = "./module"
  count = 100
  connection = var.connection
  module_name = "counted"
  instance_id = tostring(count.index)
  deduplication_enabled = var.deduplication_enabled
  deduplication_keys = var.deduplication_keys
}
module "each" {
  source = "./module"
  for_each = toset([for i in range(100) : tostring(i)])
  connection = var.connection
  module_name = "each"
  instance_id = each.key
  deduplication_enabled = var.deduplication_enabled
  deduplication_keys = var.deduplication_keys
}
''')
            terraform("get")
            before = len(events)
            terraform("plan", "-input=false", "-out=modules.tfplan")
            assert len(events) - before == 2, f"expected two module events, got {len(events) - before}"
            assert {e["properties"]["extra_data"]["module"] for e in events[before:]} == {"counted", "each"}
            before = len(events)
            result = subprocess.run(["terraform", "apply", "-input=false", "-no-color", "modules.tfplan"], cwd=work, env=env, text=True, capture_output=True)
            assert result.returncode == 0, result.stdout + result.stderr
            assert len(events) - before == 2, "saved-plan apply did not get a fresh deduplication scope"
            env["TF_VAR_deduplication_enabled"] = "false"
            before = len(events)
            terraform("plan", "-input=false")
            assert len(events) - before == 200, f"disabled deduplication sent {len(events) - before} events"
            env["TF_VAR_deduplication_enabled"] = "true"
            env["TF_VAR_deduplication_keys"] = json.dumps(["extra_data.module", "extra_data.instance"])
            before = len(events)
            terraform("plan", "-input=false")
            assert len(events) - before == 200, "instance keys did not preserve distinct instances"
            print("PASS: count=100 + for_each=100 -> 2 events per plan/saved apply; disabled deduplication or instance keys -> 200 events")
            print(f"PASS: plan ({plan_count} captures), saved-plan apply ({apply_count} captures), no-change apply with HTTP 500, typed extra_data, working-directory Git metadata, optional options.cache_enabled=false, empty resource state")
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


if __name__ == "__main__":
    main()
