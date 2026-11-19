package moonraker

import (
	"testing"
	"time"
)

// maxSummaryLen is how long the one sentence beside a setting may be. It is the
// length a settings pane prints without wrapping into a paragraph.
const maxSummaryLen = 120

// The declared default is the one the code uses when the operator writes
// nothing.
//
// An operator reads the declared default on the extension reference to learn
// what happens when they leave the key out. A declaration that names a
// different number than the code uses is documentation that lies, and nothing
// else in this module compares the two.
func TestTheDeclaredDefaultIsTheOneTheCodeUses(t *testing.T) {
	t.Parallel()

	var declared string

	for _, option := range doc().Options {
		if option.Summary == "" || len(option.Summary) > maxSummaryLen {
			t.Errorf("the summary of %s is %d characters. Write one sentence of at most %d",
				option.Key, len(option.Summary), maxSummaryLen)
		}

		if option.Key == "timeout" {
			declared = option.Default
		}
	}

	if declared == "" {
		t.Fatal("no timeout setting is declared, so an operator cannot find it")
	}

	parsed, err := time.ParseDuration(declared)
	if err != nil {
		t.Fatalf("the declared timeout %q is not a duration: %v", declared, err)
	}

	if parsed != defaultTimeout {
		t.Errorf("the settings table declares a timeout of %s and the code uses %s", parsed, defaultTimeout)
	}
}
