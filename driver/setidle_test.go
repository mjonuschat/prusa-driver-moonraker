package moonraker_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi/connectorapitest"
)

const resetFile = "SDCARD_RESET_FILE"

func idleMachine(t *testing.T) (*moonrakersim.Host, connectorapi.Session) {
	t.Helper()

	machine := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})

	return machine, connect(t, machine)
}

func setIdle(t *testing.T, session connectorapi.Session) (connectorapi.Result, error) {
	t.Helper()

	return session.Do(t.Context(), connectorapi.Command{Verb: moonraker.VerbSetIdle, Args: map[string]string{}})
}

func TestSetIdleClearsAStoppedPrint(t *testing.T) {
	t.Parallel()

	machine, session := idleMachine(t)

	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing"}})
	collect(t, session, map[string]string{"/state": `"PRINTING"`})
	machine.Push(map[string]map[string]any{"print_stats": {"state": "cancelled"}})
	collect(t, session, map[string]string{"/state": `"STOPPED"`})

	result, err := setIdle(t, session)
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Equal(t, []string{resetFile}, machine.Scripts())

	// Klipper answers the reset with standby, and nothing holds STOPPED now.
	machine.Push(map[string]map[string]any{"print_stats": {"state": "standby"}})
	collect(t, session, map[string]string{"/state": `"IDLE"`})
}

func TestSetIdleClearsAHeldEndWithoutAnotherReading(t *testing.T) {
	t.Parallel()

	machine, session := idleMachine(t)

	// A cancel macro that ends in SDCARD_RESET_FILE leaves Klipper in standby
	// while the driver keeps the end word. No reading follows a reset of a
	// machine that is already in standby, so the driver has to say idle itself.
	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing"}})
	collect(t, session, map[string]string{"/state": `"PRINTING"`})
	machine.Push(map[string]map[string]any{"print_stats": {"state": "standby"}})
	collect(t, session, map[string]string{"/state": `"STOPPED"`})

	result, err := setIdle(t, session)
	require.NoError(t, err)
	require.True(t, result.OK)

	collect(t, session, map[string]string{"/state": `"IDLE"`})
}

func TestSetIdleIsRefusedWhileAPrintIsOnTheBed(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"printing", "paused"} {
		machine, session := idleMachine(t)

		machine.Push(map[string]map[string]any{"print_stats": {"state": state}})
		collect(t, session, map[string]string{"/state": `"` + strings.ToUpper(state) + `"`})

		_, err := setIdle(t, session)
		require.Errorf(t, err, "set_idle while %s", state)
		assert.Empty(t, machine.Scripts(), "the reset would have stopped the print")
	}
}

func TestSetIdleIsRefusedWhenNothingEnded(t *testing.T) {
	t.Parallel()

	machine, session := idleMachine(t)

	_, err := setIdle(t, session)
	require.Error(t, err)
	assert.Empty(t, machine.Scripts())
}

func TestSetIdleNeedsTheVirtualSDCard(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: without(moonrakersim.DefaultObjects(), "virtual_sdcard"), Status: moonrakersim.DefaultStatus(),
	})
	session := connect(t, machine)

	resolved := connectorapitest.NewHost(nil).Resolve((&moonraker.Driver{}).Baseline(), session)
	assert.False(t, resolved.Has(moonraker.VerbSetIdle), "a machine with no virtual_sdcard has nothing to reset")
	assert.True(t, (&moonraker.Driver{}).Baseline().Has(moonraker.VerbSetIdle))
}
