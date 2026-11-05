package main

import (
	"strings"
	"testing"
)

// Checks: DOC-01, DOC-02
func TestDocMatchesTests(t *testing.T) {
	problems, err := run("../../..", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Fatalf("docs/TESTING.md and the tests disagree:\n%s", strings.Join(problems, "\n"))
	}
}
