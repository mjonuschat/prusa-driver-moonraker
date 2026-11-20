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

func extruderSettings(velocity, distance float64) map[string]any {
	return map[string]any{"extruder": map[string]any{
		"max_extrude_only_velocity": velocity, "max_extrude_only_distance": distance,
	}}
}

func TestMoveEIsOfferedWithAnExtruder(t *testing.T) {
	t.Parallel()

	caps := motionMachine(t, nil, nil)
	for _, key := range []connectorapi.Capability{
		moonraker.VerbMoveE, moonraker.ArgMoveEFeedrate, moonraker.ArgMoveELength,
	} {
		require.Contains(t, caps, key)
	}

	machine := startMachine(t, moonrakersim.Options{
		Objects: without(moonrakersim.DefaultObjects(), "extruder"), Status: moonrakersim.DefaultStatus(),
	})
	bare, _ := connect(t, machine).Capabilities()
	require.NotContains(t, bare, moonraker.VerbMoveE)
}

func TestMoveEIsBoundedByTheExtrudersOwnLimits(t *testing.T) {
	t.Parallel()

	caps := motionMachine(t, extruderSettings(106.5, 151), nil)
	assert.Equal(t, 6390.0, limitOf(t, caps, moonraker.ArgMoveEFeedrate))

	low, high := rangeOf(t, caps, moonraker.ArgMoveELength)
	assert.Equal(t, -151.0, low)
	assert.Equal(t, 151.0, high)

	slow := motionMachine(t, extruderSettings(5, 20), nil)
	assert.Equal(t, 300.0, limitOf(t, slow, moonraker.ArgMoveEFeedrate))
}

func TestMoveEWithNoLimitsInTheConfigurationUsesKlippersDefaults(t *testing.T) {
	t.Parallel()

	caps := motionMachine(t, nil, nil)
	assert.Equal(t, 600.0, limitOf(t, caps, moonraker.ArgMoveEFeedrate))

	low, high := rangeOf(t, caps, moonraker.ArgMoveELength)
	assert.Equal(t, -50.0, low, "Klipper's own max_extrude_only_distance is 50")
	assert.Equal(t, 50.0, high)
}

func TestMoveESendsAnExtrusionAndAColdRefusalComesBack(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})
	session := connect(t, machine)

	result, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbMoveE, Args: map[string]string{"length": "-5", "feedrate": "300"},
	})
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.Equal(t, []string{
		"SAVE_GCODE_STATE NAME=CONNECT_JOG\nG91\nG1 E-5 F300\nRESTORE_GCODE_STATE NAME=CONNECT_JOG",
	}, machine.Scripts())

	machine.RefuseScript("G1 E", "Extrude below minimum temp")

	result, err = session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbMoveE, Args: map[string]string{"length": "5", "feedrate": "300"},
	})
	require.NoError(t, err)
	assert.False(t, result.OK)
	assert.Equal(t, "Extrude below minimum temp", result.MachineReason)

	scripts := machine.Scripts()
	assert.Equal(t, "RESTORE_GCODE_STATE NAME=CONNECT_JOG", scripts[len(scripts)-1],
		"the coordinate mode is put back after a refusal")
}

func TestMoveEIsAcceptedOnlyWhenNoPrintIsRunning(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)
	allowed := []connectorapi.StateName{"IDLE", "PAUSED", "FINISHED", "STOPPED"}

	for _, state := range []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED", "ERROR"} {
		require.Equalf(t, slices.Contains(allowed, state), set.Accepted(state, moonraker.VerbMoveE), "in %s", state)
	}
}
