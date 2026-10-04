package app

import "testing"

func TestWindowsShow(t *testing.T) {
	ws := "/Users/me/.edrive/workspace"
	cases := map[string]bool{
		"file:///Users/me/.edrive/workspace/\n":                                 true,
		"file:///Users/me/Desktop/\nfile:///Users/me/.edrive/workspace/pass/\n": true,
		"file:///Users/me/.edrive/workspace%20old/\n":                           false,
		"file:///Users/me/Desktop/\n":                                           false,
		"":                                                                      false,
	}
	for in, want := range cases {
		if got := windowsShow(in, ws); got != want {
			t.Errorf("windowsShow(%q) = %v, want %v", in, got, want)
		}
	}
}
