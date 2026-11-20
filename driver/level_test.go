package moonraker_test

import (
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	moonraker "github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func levelMachine(t *testing.T, extra ...string) (*moonrakersim.Host, connectorapi.Session) {
	t.Helper()

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), extra...),
		Status:  moonrakersim.DefaultStatus(),
	})

	return machine, connect(t, machine)
}

func levelling(t *testing.T, session connectorapi.Session) (connectorapi.Result, error) {
	t.Helper()

	return session.Do(t.Context(), connectorapi.Command{Verb: moonraker.VerbMeshBedLeveling})
}

func TestLevelBedIsOfferedOnlyWithALevellingObject(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		objects []string
		offered bool
	}{
		{"none", nil, false},
		{"z_tilt", []string{"z_tilt"}, true},
		{"quad_gantry_level", []string{"quad_gantry_level"}, true},
		{"bed_mesh alone", []string{"bed_mesh"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, session := levelMachine(t, tc.objects...)
			caps, _ := session.Capabilities()
			assert.Equal(t, tc.offered, caps.Has(moonraker.VerbMeshBedLeveling))
		})
	}
}

func TestLevelBedRunsTheCommandOfTheMachinesObject(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		objects []string
		script  string
	}{
		{"z_tilt", []string{"z_tilt"}, "Z_TILT_ADJUST"},
		{"quad_gantry_level", []string{"quad_gantry_level"}, "QUAD_GANTRY_LEVEL"},
		{"both prefer z_tilt", []string{"quad_gantry_level", "z_tilt"}, "Z_TILT_ADJUST"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			machine, session := levelMachine(t, tc.objects...)

			result, err := levelling(t, session)
			require.NoError(t, err)
			assert.True(t, result.OK)
			assert.Equal(t, []string{tc.script}, machine.Scripts())
		})
	}
}

func TestARefusedLevelIsReportedWithKlippersMessage(t *testing.T) {
	t.Parallel()

	machine, session := levelMachine(t, "z_tilt")
	machine.RefuseScript("Z_TILT_ADJUST", "Must home axis first")

	result, err := levelling(t, session)
	require.NoError(t, err)
	assert.False(t, result.OK)
	assert.Equal(t, "Must home axis first", result.MachineReason)
	assert.Len(t, machine.Scripts(), 1, "no restore follows a refused level")
}

func TestLevelBedIsRefusedWhilePrinting(t *testing.T) {
	t.Parallel()

	machine, session := levelMachine(t, "z_tilt")

	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing"}})
	collect(t, session, map[string]string{"/state": `"PRINTING"`})

	_, err := levelling(t, session)
	require.Error(t, err)
	require.True(t, fault.HasCode(err, fault.PrinterVerbNotAllowedInState.Code()))
	assert.Empty(t, machine.Scripts())
}

func TestASlowScriptIsWaitedForByLevellingAndHomingButNotByAJog(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), "z_tilt"),
		Status:  moonrakersim.DefaultStatus(),
	})
	machine.SlowScripts(300 * time.Millisecond)

	session, err := moonraker.NewDriver(50*time.Millisecond).
		Connect(t.Context(), connectorapi.Endpoint{Address: machine.Address()}, moonraker.NewDriver(0).Baseline())
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	result, err := levelling(t, session)
	require.NoError(t, err)
	assert.True(t, result.OK)

	result, err = session.Do(t.Context(), connectorapi.Command{Verb: moonraker.VerbHome})
	require.NoError(t, err)
	assert.True(t, result.OK)

	_, err = session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbMoveZ, Args: map[string]string{"distance": "1", "feedrate": "600"},
	})
	require.Error(t, err, "a jog is held to the one-call timeout")
}

func TestLevelBedIsRefusedBeforeAnyStateIsKnown(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(without(without(moonrakersim.DefaultObjects(), "print_stats"), "webhooks"), "z_tilt"),
	})

	session := connect(t, machine)

	_, err := levelling(t, session)
	require.Error(t, err)
	require.True(t, fault.HasCode(err, fault.PrinterVerbNotAllowedInState.Code()))
	assert.Empty(t, machine.Scripts())
}
