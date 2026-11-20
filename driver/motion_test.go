package moonraker_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
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
