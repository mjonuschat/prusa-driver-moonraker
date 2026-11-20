package moonraker_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func TestACancelledPrintThatKlipperResetsStaysStopped(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})
	session := connect(t, machine)

	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing"}})
	collect(t, session, map[string]string{"/state": `"PRINTING"`})
	machine.Push(map[string]map[string]any{"print_stats": {"state": "cancelled"}})
	collect(t, session, map[string]string{"/state": `"STOPPED"`})
	machine.Push(map[string]map[string]any{"print_stats": {"state": "standby"}})
	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing"}})

	var seen []string

	patches := session.State()
	deadline := time.After(settle)

	for len(seen) == 0 || seen[len(seen)-1] != `"PRINTING"` {
		select {
		case patch := <-patches:
			if patch.Path == "/state" {
				seen = append(seen, string(patch.Value))
			}
		case <-deadline:
			t.Fatalf("no PRINTING after %v", seen)
		}
	}

	require.NotContains(t, seen, `"IDLE"`, "the reset must not read as idle")
	assert.Equal(t, `"PRINTING"`, seen[len(seen)-1])
}
