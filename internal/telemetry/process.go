package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// Process identifies a running process. Start distinguishes processes that
// reuse the same ID over time. Start and Executable are empty when unavailable.
type Process struct {
	ID         int
	Start      string
	Executable string
}

// parentProcess returns the process that started this provider, which is the
// Terraform or OpenTofu CLI when Terraform launches the provider.
func parentProcess() (Process, bool) {
	pid := os.Getppid()
	if pid <= 1 {
		return Process{}, false
	}
	parent := Process{ID: pid}
	parent.Start, _ = processStart(pid)
	parent.Executable, _ = processExecutable(pid)
	return parent, true
}

// commandID identifies the command run by the parent process without exposing
// its process ID. All provider processes of one command share it.
func commandID(parent Process) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("terraform-provider-telemetry/command:%d:%s", parent.ID, parent.Start)))
	return hex.EncodeToString(sum[:16])
}
