package rclone

import (
	"testing"
)

func TestParseDiffOutputShape(t *testing.T) {
	// Keep the expected rclone check symbols documented by this package.
	input := []string{
		"= same.txt",
		"+ local-only.txt",
		"- remote-only.txt",
		"* changed.txt",
		"! broken.txt",
	}

	var local, remote, changed, failed int
	for _, line := range input {
		switch line[0] {
		case '+':
			local++
		case '-':
			remote++
		case '*':
			changed++
		case '!':
			failed++
		}
	}
	if local != 1 || remote != 1 || changed != 1 || failed != 1 {
		t.Fatalf("unexpected diff classification: local=%d remote=%d changed=%d failed=%d", local, remote, changed, failed)
	}
}
