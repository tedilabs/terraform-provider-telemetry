package telemetry

import "testing"

func TestParseProcArgs(t *testing.T) {
	for _, test := range []struct {
		data string
		path string
		ok   bool
	}{
		{"\x02\x00\x00\x00/usr/local/bin/terraform\x00\x00\x00terraform\x00plan\x00", "/usr/local/bin/terraform", true},
		{"\x01\x00\x00\x00/bin/tofu", "/bin/tofu", true},
		{"\x01\x00\x00\x00\x00", "", false},
		{"\x01\x00", "", false},
	} {
		if path, ok := parseProcArgs([]byte(test.data)); path != test.path || ok != test.ok {
			t.Errorf("%q: got (%q, %v), want (%q, %v)", test.data, path, ok, test.path, test.ok)
		}
	}
}
