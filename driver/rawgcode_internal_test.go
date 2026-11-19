package moonraker

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func TestGcodeLineAcceptsAndTrims(t *testing.T) {
	t.Parallel()

	unchanged := []string{
		"G28", "M104 S215", `M862.3 P "MK4S"`, "T1", "M117 Ready: 2/3 (~4m)", "M500", "M112",
		"M104 S215 ; warm up", "M104S215", "M862. P1", "M2830",
	}
	for _, payload := range unchanged {
		line, err := gcodeLine(payload)
		require.NoError(t, err, payload)
		require.Equal(t, payload, line)
	}

	line, err := gcodeLine("  G28 X  ")
	require.NoError(t, err)
	require.Equal(t, "G28 X", line)
}

func TestGcodeLineRefusals(t *testing.T) {
	t.Parallel()

	notAllowed := []string{"M28", "M28.1", "m28", "M29 file", "M30 file"}
	for _, payload := range notAllowed {
		_, err := gcodeLine(payload)
		require.True(t, errors.Is(err, connectorapi.GCodeNotAllowed.New("")), "%q: %v", payload, err)
	}

	malformed := []string{
		"", "   ", "G28\nG29", "G28\r", "G28 \xc3\xa9", "X10", "G", "HOME",
		"SET_HEATER_TEMPERATURE HEATER=extruder TARGET=200",
	}
	for _, payload := range malformed {
		_, err := gcodeLine(payload)
		require.True(t, errors.Is(err, connectorapi.GCodeMalformed.New("")), "%q: %v", payload, err)
	}
}

func TestEmergencyStopTakesOnlyM112(t *testing.T) {
	t.Parallel()

	require.NoError(t, emergencyStop("M112"))
	require.NoError(t, emergencyStop(" M112 "))

	for _, payload := range []string{"M112 S1", "m112", "G28", ""} {
		err := emergencyStop(payload)
		require.True(t, errors.Is(err, connectorapi.GCodeNotAllowed.New("")), "%q: %v", payload, err)
	}
}
