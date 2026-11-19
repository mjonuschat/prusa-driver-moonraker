package moonraker

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A host that falls behind must still be given the machine's latest reading.
// connectorapi.Session.State says a driver whose queue is full drops the
// oldest patch: telemetry is a repeating snapshot, so the newest value for a
// path is the one the host wants, and dropping the newest holds the host's
// tree at a stale value for as long as the host stays behind.
func TestAFullInboxDropsTheOldestPatchAndKeepsTheNewest(t *testing.T) {
	t.Parallel()

	session := &Session{patches: make(chan connectorapi.StatePatch, 2), done: make(chan struct{})}

	for _, value := range []string{"1", "2", "3", "4"} {
		session.report(connectorapi.StatePatch{Path: "temperature", Value: []byte(value)})
	}

	if got := session.Dropped(); got != 2 {
		t.Fatalf("the session counted %d dropped patches, want 2", got)
	}

	var held []string
	for range 2 {
		held = append(held, string((<-session.patches).Value))
	}

	if held[len(held)-1] != "4" {
		t.Fatalf("the queue holds %v, and the newest reading the machine sent is not in it", held)
	}

	if held[0] != "3" {
		t.Fatalf("the queue holds %v, want the two newest readings", held)
	}
}
