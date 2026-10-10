package telemetry

import (
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestParentProcess(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("parent process start time is not supported on " + runtime.GOOS)
	}
	first, ok := parentProcess()
	if !ok || first.ID != os.Getppid() || first.Start == "" {
		t.Fatalf("parent process not found: %+v, %v", first, ok)
	}
	if second, ok := parentProcess(); !ok || second != first {
		t.Fatalf("parent process changed: %+v, %+v", first, second)
	}
}

func TestCommandID(t *testing.T) {
	parent := Process{ID: 4242, Start: "1700000000.000001"}
	id := commandID(parent)
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) || id != commandID(parent) {
		t.Fatalf("unstable or malformed command ID: %s", id)
	}
	if strings.Contains(id, strconv.Itoa(parent.ID)) {
		t.Fatal("command ID exposes the process ID")
	}
	for _, other := range []Process{{ID: 4243, Start: parent.Start}, {ID: parent.ID, Start: "1700000000.000002"}} {
		if commandID(other) == id {
			t.Fatalf("different processes share a command ID: %+v", other)
		}
	}
}

func TestTerraformCommandID(t *testing.T) {
	c := NewCollector()
	c.Getenv = func(key string) string { return map[string]string{"TF_WORKSPACE": "prod"}[key] }
	c.Parent = func() (Process, bool) { return Process{ID: 4242, Start: "start"}, true }
	if got := c.terraform(); got["command_id"] != commandID(Process{ID: 4242, Start: "start"}) || got["workspace"] != "prod" {
		t.Fatalf("missing command ID: %v", got)
	}
	c.Parent = func() (Process, bool) { return Process{}, false }
	if _, ok := c.terraform()["command_id"]; ok {
		t.Fatal("command ID without a parent process")
	}
}
