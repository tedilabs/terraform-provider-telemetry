terraform {
  required_providers {
    telemetry = {
      source = "tedilabs/telemetry"
    }
  }
}

# The test harness requires an explicit provider configuration, even for functions.
provider "telemetry" {}

variable "host" { type = string }
variable "deduplication_enabled" { type = bool }
variable "deduplication_keys" { type = list(string) }

module "counted" {
  source                = "./testdata/acceptance/deduplication/module"
  count                 = 100
  host                  = var.host
  module_name           = "counted"
  instance_id           = tostring(count.index)
  deduplication_enabled = var.deduplication_enabled
  deduplication_keys    = var.deduplication_keys
}

module "each" {
  source                = "./testdata/acceptance/deduplication/module"
  for_each              = toset([for i in range(100) : tostring(i)])
  host                  = var.host
  module_name           = "each"
  instance_id           = each.key
  deduplication_enabled = var.deduplication_enabled
  deduplication_keys    = var.deduplication_keys
}

# Keep check results visible in `terraform show -json` for this resource-free state.
output "tested" { value = true }
