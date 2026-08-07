package loglevel

import "testing"

func TestVerbose(t *testing.T) {
	cases := []struct {
		name  string
		value string
		unset bool
		want  bool
	}{
		{"unset defaults to verbose", "", true, true},
		{"empty string defaults to verbose", "", false, true},
		{"INFO is verbose", "INFO", false, true},
		{"DEBUG is verbose", "DEBUG", false, true},
		{"lowercase info is verbose", "info", false, true},
		{"mixed case Debug is verbose", "Debug", false, true},
		{"WARN is silent", "WARN", false, false},
		{"ERROR is silent", "ERROR", false, false},
		{"garbage value is silent", "whatever", false, false},
		{"padded value is trimmed", "  INFO  ", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.unset {
				t.Setenv("LOG_LEVEL", "")
				// t.Setenv can't unset, so simulate "unset" with empty,
				// which Verbose() already treats identically to unset.
			} else {
				t.Setenv("LOG_LEVEL", c.value)
			}
			if got := Verbose(); got != c.want {
				t.Fatalf("Verbose() with LOG_LEVEL=%q = %v, want %v", c.value, got, c.want)
			}
		})
	}
}
