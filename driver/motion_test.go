package moonraker_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	moonraker "github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

var motionKeys = []connectorapi.Capability{
	moonraker.VerbMove, moonraker.ArgMoveFeedrate, moonraker.ArgMoveX, moonraker.ArgMoveY,
	moonraker.VerbMoveZ, moonraker.ArgMoveZFeedrate, moonraker.ArgMoveZDistance,
	moonraker.VerbHome, moonraker.ArgHomeAxis, moonraker.VerbDisableSteppers,
}

func motionMachine(
	t *testing.T, settings map[string]any, profile func(connectorapi.Set) connectorapi.Set,
) connectorapi.Set {
	t.Helper()

	status := moonrakersim.DefaultStatus()
	if settings != nil {
		status["configfile"] = map[string]any{"settings": settings}
	}

	machine := startMachine(t, moonrakersim.Options{Objects: moonrakersim.DefaultObjects(), Status: status})
	driver := moonraker.NewDriver(0)

	offered := driver.Baseline()
	if profile != nil {
		offered = profile(offered)
	}

	session, err := driver.Connect(t.Context(), connectorapi.Endpoint{Address: machine.Address()}, offered)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	caps, _ := session.Capabilities()

	return caps
}

func limitOf(t *testing.T, caps connectorapi.Set, key connectorapi.Capability) float64 {
	t.Helper()

	limit, ok := caps.Limit(key)
	require.True(t, ok, "%s has a limit", key)

	return limit
}

func rangeOf(t *testing.T, caps connectorapi.Set, key connectorapi.Capability) (float64, float64) {
	t.Helper()

	declared := caps[key]
	require.True(t, declared.HasMin, "%s has a minimum", key)

	return declared.Min, limitOf(t, caps, key)
}

func TestAToolheadMachineIsOfferedTheFourMotionVerbs(t *testing.T) {
	t.Parallel()

	caps := motionMachine(t, nil, nil)
	for _, key := range motionKeys {
		require.Contains(t, caps, key)
	}
}

func TestAMachineWithNoToolheadIsOfferedNoMotion(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: without(moonrakersim.DefaultObjects(), "toolhead"),
		Status:  moonrakersim.DefaultStatus(),
	})

	caps, _ := connect(t, machine).Capabilities()
	for _, key := range motionKeys {
		require.NotContains(t, caps, key)
	}
}

func TestFeedrateIsBoundedByTheMachinesOwnVelocities(t *testing.T) {
	t.Parallel()

	caps := motionMachine(t, map[string]any{
		"printer": map[string]any{"max_velocity": 400.0, "max_z_velocity": 20.0},
	}, nil)

	require.InDelta(t, 24000, limitOf(t, caps, moonraker.ArgMoveFeedrate), 0)
	require.InDelta(t, 1200, limitOf(t, caps, moonraker.ArgMoveZFeedrate), 0)
}

func TestAMissingVelocityLeavesTheConservativeFallback(t *testing.T) {
	t.Parallel()

	caps := motionMachine(t, map[string]any{}, nil)
	require.InDelta(t, 6000, limitOf(t, caps, moonraker.ArgMoveFeedrate), 0)
	require.InDelta(t, 600, limitOf(t, caps, moonraker.ArgMoveZFeedrate), 0)

	caps = motionMachine(t, map[string]any{"printer": map[string]any{"max_velocity": 400.0}}, nil)
	require.InDelta(t, 24000, limitOf(t, caps, moonraker.ArgMoveFeedrate), 0)
	require.InDelta(t, 600, limitOf(t, caps, moonraker.ArgMoveZFeedrate), 0)
}

func TestAMachineLimitAboveTheFallbackIsAdvertisedInFull(t *testing.T) {
	t.Parallel()

	settings := map[string]any{
		"printer":   map[string]any{"max_velocity": 400.0, "max_z_velocity": 20.0},
		"stepper_x": map[string]any{"position_min": 0.0, "position_max": 2000.0},
	}

	caps := motionMachine(t, settings, nil)
	require.InDelta(t, 24000, limitOf(t, caps, moonraker.ArgMoveFeedrate), 0)
	require.InDelta(t, 1200, limitOf(t, caps, moonraker.ArgMoveZFeedrate), 0)

	low, high := rangeOf(t, caps, moonraker.ArgMoveX)
	require.InDelta(t, -2000, low, 0)
	require.InDelta(t, 2000, high, 0)

	caps = motionMachine(t, settings, func(set connectorapi.Set) connectorapi.Set {
		value := set[moonraker.ArgMoveFeedrate]
		value.Limit = 12000
		set[moonraker.ArgMoveFeedrate] = value

		return set
	})
	require.InDelta(t, 12000, limitOf(t, caps, moonraker.ArgMoveFeedrate), 0)
}

func TestDistancesAreBoundedSymmetricallyByTheAxisSpan(t *testing.T) {
	t.Parallel()

	caps := motionMachine(t, nil, nil)

	for key, span := range map[connectorapi.Capability]float64{
		moonraker.ArgMoveX: 300, moonraker.ArgMoveY: 305, moonraker.ArgMoveZDistance: 280,
	} {
		low, high := rangeOf(t, caps, key)
		require.InDelta(t, -span, low, 0, key)
		require.InDelta(t, span, high, 0, key)
	}

	caps = motionMachine(t, map[string]any{}, nil)

	for _, key := range []connectorapi.Capability{moonraker.ArgMoveX, moonraker.ArgMoveY, moonraker.ArgMoveZDistance} {
		low, high := rangeOf(t, caps, key)
		require.InDelta(t, -1000, low, 0, key)
		require.InDelta(t, 1000, high, 0, key)
	}
}

func TestANegativeStepIsValidOnAnAxisThatStartsAtZero(t *testing.T) {
	t.Parallel()

	caps := motionMachine(t, nil, nil)

	_, err := caps.Validate(connectorapi.Command{
		Verb: moonraker.VerbMove, Args: map[string]string{"x": "-10", "feedrate": "3000"},
	})
	require.NoError(t, err)

	_, err = caps.Validate(connectorapi.Command{
		Verb: moonraker.VerbMove, Args: map[string]string{"x": "-301", "feedrate": "3000"},
	})
	require.Error(t, err)
}

const restoreLine = "RESTORE_GCODE_STATE NAME=CONNECT_JOG"

var jogScript = strings.Join([]string{
	"SAVE_GCODE_STATE NAME=CONNECT_JOG", "G91", "G1 X1.5 F3000", restoreLine,
}, "\n")

func readyMachine(t *testing.T) (*moonrakersim.Host, connectorapi.Session) {
	t.Helper()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)
	collect(t, session, map[string]string{"/state": `"IDLE"`})

	return machine, session
}

func motionCommands() []connectorapi.Command {
	return []connectorapi.Command{
		{Verb: moonraker.VerbMove, Args: map[string]string{"x": "1.5", "feedrate": "3000"}},
		{Verb: moonraker.VerbMoveZ, Args: map[string]string{"distance": "-0.5", "feedrate": "600"}},
		{Verb: moonraker.VerbHome, Args: map[string]string{}},
		{Verb: moonraker.VerbDisableSteppers, Args: map[string]string{}},
	}
}

func TestAJogIsSentAsOneScriptAndSucceeds(t *testing.T) {
	t.Parallel()

	machine, session := readyMachine(t)

	result, err := session.Do(t.Context(), motionCommands()[0])
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Equal(t, []string{jogScript}, machine.Scripts())
}

func TestMoveZHomeAndStepperVerbsSendTheirScripts(t *testing.T) {
	t.Parallel()

	machine, session := readyMachine(t)

	cmds := []connectorapi.Command{
		motionCommands()[1],
		motionCommands()[2],
		{Verb: moonraker.VerbHome, Args: map[string]string{"axis": "xy"}},
		motionCommands()[3],
	}
	for _, cmd := range cmds {
		result, err := session.Do(t.Context(), cmd)
		require.NoError(t, err)
		require.True(t, result.OK)
	}

	require.Equal(t, []string{
		strings.Join([]string{"SAVE_GCODE_STATE NAME=CONNECT_JOG", "G91", "G1 Z-0.5 F600", restoreLine}, "\n"),
		"G28", "G28 X Y", "M84",
	}, machine.Scripts())
}

func TestARefusedJogSendsTheRestoreAndReturnsTheRefusal(t *testing.T) {
	t.Parallel()

	machine, session := readyMachine(t)
	machine.RefuseScript("G1", "Must home axis first")

	result, err := session.Do(t.Context(), motionCommands()[0])
	require.NoError(t, err)
	require.False(t, result.OK)
	require.Equal(t, "Must home axis first", result.MachineReason)

	scripts := machine.Scripts()
	require.Len(t, scripts, 2)
	require.Equal(t, restoreLine, scripts[1])
}

func TestARefusedHomeSendsNoRestore(t *testing.T) {
	t.Parallel()

	machine, session := readyMachine(t)
	machine.RefuseScript("G28", "Homing failed")

	result, err := session.Do(t.Context(), motionCommands()[2])
	require.NoError(t, err)
	require.False(t, result.OK)
	require.Len(t, machine.Scripts(), 1)
}

func requireAllRefused(t *testing.T, session connectorapi.Session, machine *moonrakersim.Host) {
	t.Helper()

	for _, cmd := range motionCommands() {
		_, err := session.Do(t.Context(), cmd)
		require.True(t, fault.HasCode(err, fault.PrinterVerbNotAllowedInState.Code()), "%s: %v", cmd.Verb, err)
	}

	require.Empty(t, machine.Scripts())
}

func TestMotionIsRefusedWhilePrintingAndInError(t *testing.T) {
	t.Parallel()

	machine, session := readyMachine(t)

	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing"}})
	collect(t, session, map[string]string{"/state": `"PRINTING"`})
	requireAllRefused(t, session, machine)

	machine.Push(map[string]map[string]any{"webhooks": {"state": "shutdown", "state_message": "x"}})
	collect(t, session, map[string]string{"/state": `"ERROR"`})
	requireAllRefused(t, session, machine)
}

func TestMotionIsAllowedWhilePausedAndWhenStopped(t *testing.T) {
	t.Parallel()

	machine, session := readyMachine(t)

	for state, word := range map[string]string{"paused": "PAUSED", "cancelled": "STOPPED"} {
		machine.Push(map[string]map[string]any{"print_stats": {"state": state}})
		collect(t, session, map[string]string{"/state": `"` + word + `"`})

		before := len(machine.Scripts())

		for _, cmd := range motionCommands() {
			result, err := session.Do(t.Context(), cmd)
			require.NoError(t, err)
			require.True(t, result.OK)
		}

		require.Greater(t, len(machine.Scripts()), before)
	}
}

func TestTwoJogsSentTogetherDoNotShareARecovery(t *testing.T) {
	t.Parallel()

	machine, session := readyMachine(t)
	machine.RefuseScript("G1", "Must home axis first")
	machine.SlowScripts(50 * time.Millisecond)

	var wg sync.WaitGroup

	for range 2 {
		wg.Go(func() {
			_, err := session.Do(t.Context(), motionCommands()[0])
			assert.NoError(t, err)
		})
	}

	wg.Wait()

	scripts := machine.Scripts()
	require.Len(t, scripts, 4)

	for i := 0; i < len(scripts); i += 2 {
		require.Contains(t, scripts[i], "G1")
		require.Equal(t, restoreLine, scripts[i+1])
	}
}

func TestMotionIsRefusedBeforeAnyStateIsKnown(t *testing.T) {
	t.Parallel()

	objects := without(without(moonrakersim.DefaultObjects(), "print_stats"), "webhooks")
	machine := startMachine(t, moonrakersim.Options{Objects: objects})
	session := connect(t, machine)

	requireAllRefused(t, session, machine)

	_, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbSendGCode, Args: map[string]string{"gcode": "G28"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"G28"}, machine.Scripts())
}

func TestAJogNeverUsesTheStateAPauseSaves(t *testing.T) {
	t.Parallel()

	machine, session := readyMachine(t)

	for _, cmd := range motionCommands() {
		_, err := session.Do(t.Context(), cmd)
		require.NoError(t, err)
	}

	for _, script := range machine.Scripts() {
		require.NotContains(t, strings.ToUpper(script), "PAUSE_STATE")
	}
}

func TestResumeWaitsForAJogAndItsRecovery(t *testing.T) {
	t.Parallel()

	machine, session := readyMachine(t)
	machine.Push(map[string]map[string]any{"print_stats": {"state": "paused"}})
	collect(t, session, map[string]string{"/state": `"PAUSED"`})
	machine.RefuseScript("G1", "Must home axis first")
	machine.SlowScripts(100 * time.Millisecond)

	var wg sync.WaitGroup

	wg.Go(func() {
		_, err := session.Do(t.Context(), motionCommands()[0])
		assert.NoError(t, err)
	})

	time.Sleep(30 * time.Millisecond)

	_, err := session.Do(t.Context(), connectorapi.Command{Verb: moonraker.VerbResumePrint})
	require.NoError(t, err)
	wg.Wait()

	calls := machine.Calls()
	require.Equal(t, "printer.print.resume", calls[len(calls)-1])
	require.Len(t, machine.Scripts(), 2)
}
