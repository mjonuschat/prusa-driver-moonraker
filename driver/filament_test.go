package moonraker_test

import (
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	moonraker "github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func filamentMachine(t *testing.T, macros ...string) (*moonrakersim.Host, connectorapi.Session) {
	t.Helper()

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), macros...), Status: moonrakersim.DefaultStatus(),
	})

	return machine, connect(t, machine)
}

func TestFilamentVerbsAreOfferedPerMacro(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		macros       []string
		load, unload bool
	}{
		{"none", nil, false, false},
		{"named", []string{"gcode_macro LOAD_FILAMENT", "gcode_macro UNLOAD_FILAMENT"}, true, true},
		{"marlin", []string{"gcode_macro M701", "gcode_macro M702"}, true, true},
		{"load only", []string{"gcode_macro LOAD_FILAMENT"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, session := filamentMachine(t, tc.macros...)
			caps, _ := session.Capabilities()
			assert.Equal(t, tc.load, caps.Has(moonraker.VerbLoadFilament))
			assert.Equal(t, tc.unload, caps.Has(moonraker.VerbUnloadFilament))
		})
	}
}

func TestFilamentVerbsRunTheMachinesMacroWithNoParameters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		macros       []string
		load, unload string
	}{
		{"named", []string{"gcode_macro LOAD_FILAMENT", "gcode_macro UNLOAD_FILAMENT"}, "LOAD_FILAMENT", "UNLOAD_FILAMENT"},
		{"marlin", []string{"gcode_macro M701", "gcode_macro M702"}, "M701", "M702"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			machine, session := filamentMachine(t, tc.macros...)

			for _, verb := range []connectorapi.Capability{moonraker.VerbLoadFilament, moonraker.VerbUnloadFilament} {
				result, err := session.Do(t.Context(), connectorapi.Command{Verb: verb})
				require.NoError(t, err)
				assert.True(t, result.OK)
			}

			assert.Equal(t, []string{tc.load, tc.unload}, machine.Scripts())
		})
	}
}

func TestAMacroRefusalIsReturnedAsItIs(t *testing.T) {
	t.Parallel()

	machine, session := filamentMachine(t, "gcode_macro LOAD_FILAMENT")
	machine.RefuseScript("LOAD_FILAMENT", "Extruder too cold")

	result, err := session.Do(t.Context(), connectorapi.Command{Verb: moonraker.VerbLoadFilament})
	require.NoError(t, err)
	assert.False(t, result.OK)
	assert.Equal(t, "Extruder too cold", result.MachineReason)
}

func TestFilamentVerbsAreAcceptedWhenNotPrinting(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)
	allowed := []connectorapi.StateName{"IDLE", "PAUSED", "FINISHED", "STOPPED"}

	for _, verb := range []connectorapi.Capability{moonraker.VerbLoadFilament, moonraker.VerbUnloadFilament} {
		for _, state := range []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED", "ERROR"} {
			require.Equalf(t, slices.Contains(allowed, state), set.Accepted(state, verb), "%s in %s", verb, state)
		}
	}
}

func TestFilamentVerbsAreRefusedWhilePrinting(t *testing.T) {
	t.Parallel()

	machine, session := filamentMachine(t, "gcode_macro LOAD_FILAMENT")

	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing"}})
	collect(t, session, map[string]string{"/state": `"PRINTING"`})

	_, err := session.Do(t.Context(), connectorapi.Command{Verb: moonraker.VerbLoadFilament})
	require.Error(t, err)
	assert.Empty(t, machine.Scripts())
}
