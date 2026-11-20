package moonraker_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	moonraker "github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func TestSpeedAndFlowAreOfferedWithPercentBounds(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})
	caps, _ := connect(t, machine).Capabilities()

	for _, pair := range [][2]connectorapi.Capability{
		{moonraker.VerbSetSpeed, moonraker.ArgSpeed},
		{moonraker.VerbSetFlow, moonraker.ArgFlow},
	} {
		require.Contains(t, caps, pair[0])
		require.Contains(t, caps, pair[1])

		declared := caps[pair[1]]
		assert.Equal(t, connectorapi.KindNumber, declared.Kind)
		assert.True(t, declared.Required)
		assert.Equal(t, connectorapi.UnitPercent, declared.Unit)
		assert.Equal(t, 20.0, declared.Min)
		assert.Equal(t, 999.0, limitOf(t, caps, pair[1]))
	}
}

func TestSpeedAndFlowAreNotOfferedWithoutGcodeMove(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: without(moonrakersim.DefaultObjects(), "gcode_move"), Status: moonrakersim.DefaultStatus(),
	})
	caps, _ := connect(t, machine).Capabilities()

	for _, key := range []connectorapi.Capability{
		moonraker.VerbSetSpeed, moonraker.ArgSpeed, moonraker.VerbSetFlow, moonraker.ArgFlow,
	} {
		require.NotContains(t, caps, key)
	}
}

func TestSpeedAndFlowSendM220AndM221(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})
	session := connect(t, machine)

	for _, cmd := range []connectorapi.Command{
		{Verb: moonraker.VerbSetSpeed, Args: map[string]string{"speed": "120"}},
		{Verb: moonraker.VerbSetFlow, Args: map[string]string{"flow": "97.5"}},
	} {
		result, err := session.Do(t.Context(), cmd)
		require.NoError(t, err)
		require.True(t, result.OK)
	}

	assert.Equal(t, []string{"M220 S120", "M221 S97.5"}, machine.Scripts())
}

func TestSpeedAndFlowAreAcceptedInEveryStateButError(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)

	for _, verb := range []connectorapi.Capability{moonraker.VerbSetSpeed, moonraker.VerbSetFlow} {
		for _, state := range []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED"} {
			require.Truef(t, set.Accepted(state, verb), "%s in %s", verb, state)
		}

		require.Falsef(t, set.Accepted("ERROR", verb), "%s in ERROR", verb)
	}
}

func TestASpeedChangeIsSentWhileAPrintRuns(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})
	session := connect(t, machine)

	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing"}})
	collect(t, session, map[string]string{"/state": `"PRINTING"`})

	result, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbSetSpeed, Args: map[string]string{"speed": "80"},
	})
	require.NoError(t, err)
	require.True(t, result.OK)
	assert.Equal(t, []string{"M220 S80"}, machine.Scripts())
}
