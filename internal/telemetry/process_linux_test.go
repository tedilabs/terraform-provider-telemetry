package telemetry

import "testing"

func TestParseProcStatStart(t *testing.T) {
	fields := "S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 987654 20 21"
	for _, test := range []struct {
		stat, start string
		ok          bool
	}{
		{"4242 (terraform) " + fields, "987654", true},
		{"4242 (odd) name (x)) " + fields, "987654", true},
		{"4242 (terraform) S 1 2", "", false},
		{"no parenthesis", "", false},
	} {
		if start, ok := parseProcStatStart(test.stat); start != test.start || ok != test.ok {
			t.Errorf("%q: got (%q, %v), want (%q, %v)", test.stat, start, ok, test.start, test.ok)
		}
	}
}
