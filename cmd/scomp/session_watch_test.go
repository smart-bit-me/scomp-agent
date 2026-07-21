package main

import "testing"

func TestIsTmuxSessionChange(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"%sessions-changed", true},
		{"%session-changed $1 work", true},
		{"%session-renamed $1 renamed", true},
		{"%window-add @2", false},
		{"%output %1 hello", false},
	}
	for _, test := range tests {
		if got := isTmuxSessionChange(test.line); got != test.want {
			t.Errorf("isTmuxSessionChange(%q) = %v, want %v", test.line, got, test.want)
		}
	}
}
