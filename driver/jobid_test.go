package moonraker_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

// TestAPrintKeepsItsIDAcrossARedial pins the id to Klipper's clock: the driver
// dials again some time later and must still name the same print.
func TestAPrintKeepsItsIDAcrossARedial(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	machine.SetEventTime(1000)
	machine.Push(map[string]map[string]any{"print_stats": {
		"state": "printing", "filename": "benchy.gcode", "total_duration": 75,
	}})

	wanted := map[string]string{"/state": `"PRINTING"`, "/job/name": `"benchy.gcode"`}
	first := collect(t, connect(t, machine), wanted)["/job/id"]

	machine.HangUp()
	machine.SetEventTime(1030)
	machine.Push(map[string]map[string]any{"print_stats": {"total_duration": 105}})

	second := collect(t, connect(t, machine), wanted)["/job/id"]

	require.NotEmpty(t, first)
	require.Equal(t, first, second)
}
