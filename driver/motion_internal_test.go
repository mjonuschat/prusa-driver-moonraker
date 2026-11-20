package moonraker

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func jogText(move string) string {
	return "SAVE_GCODE_STATE NAME=CONNECT_JOG\nG91\n" + move + "\nRESTORE_GCODE_STATE NAME=CONNECT_JOG"
}

func TestMoveScriptWritesOnlyTheAxesGiven(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args map[string]string
		move string
	}{
		{map[string]string{"x": "1.5", "feedrate": "3000"}, "G1 X1.5 F3000"},
		{map[string]string{"y": "-2", "feedrate": "600"}, "G1 Y-2 F600"},
		{map[string]string{"x": "1", "y": "2", "feedrate": "60"}, "G1 X1 Y2 F60"},
	}
	for _, c := range cases {
		got, err := moveScript(c.args)
		require.NoError(t, err)
		require.Equal(t, jogText(c.move), got)
	}
}

func TestMoveScriptRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()

	cases := []map[string]string{
		{"feedrate": "3000"},
		{"x": "1"},
		{"x": "1", "feedrate": "0"},
		{"x": "1", "feedrate": "-5"},
		{"x": "1", "feedrate": "1.5"},
		{"x": "1", "feedrate": "1e3"},
		{"x": "1", "feedrate": "abc"},
		{"x": "NaN", "feedrate": "60"},
		{"x": "Inf", "feedrate": "60"},
		{"x": "abc", "feedrate": "60"},
	}
	for _, args := range cases {
		_, err := moveScript(args)
		require.Error(t, err, "%v", args)
	}
}

func TestNumbersAreWrittenInPlainDecimal(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"0.0000001": "0.0000001",
		"1e21":      "1000000000000000000000",
		"-0":        "0",
		"10":        "10",
	}
	for in, want := range cases {
		got, err := formatMillimetres(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got)
	}
	for _, bad := range []string{"NaN", "Inf", "-Inf", "abc", ""} {
		_, err := formatMillimetres(bad)
		require.Error(t, err, bad)
	}
}

func TestMoveZScript(t *testing.T) {
	t.Parallel()

	got, err := moveZScript(map[string]string{"distance": "-0.5", "feedrate": "600"})
	require.NoError(t, err)
	require.Equal(t, jogText("G1 Z-0.5 F600"), got)

	_, err = moveZScript(map[string]string{"feedrate": "600"})
	require.Error(t, err)
	_, err = moveZScript(map[string]string{"distance": "1"})
	require.Error(t, err)
}

func TestHomeScript(t *testing.T) {
	t.Parallel()

	got, err := homeScript(nil)
	require.NoError(t, err)
	require.Equal(t, "G28", got)

	cases := map[string]string{
		"": "G28", "  ": "G28", "x": "G28 X", "X": "G28 X", "xy": "G28 X Y", "YX": "G28 X Y",
		"x y": "G28 X Y", "x,y": "G28 X Y", "zzx": "G28 X Z",
	}
	for in, want := range cases {
		got, err := homeScript(map[string]string{"axis": in})
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, in := range []string{"q", "xq", "x;M112", "G28"} {
		_, err := homeScript(map[string]string{"axis": in})
		require.Error(t, err, in)
		require.True(t, errors.Is(err, connectorapi.GCodeMalformed.New("")), in)
	}
}

func TestNoScriptCarriesALineBreakInAnArgument(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"1\nM112", "1;M112", "1\r", "1\x00"} {
		_, err := moveScript(map[string]string{"x": bad, "feedrate": "60"})
		require.Error(t, err, bad)
		_, err = moveScript(map[string]string{"x": "1", "feedrate": bad})
		require.Error(t, err, bad)
		_, err = moveZScript(map[string]string{"distance": bad, "feedrate": "60"})
		require.Error(t, err, bad)
		_, err = moveZScript(map[string]string{"distance": "1", "feedrate": bad})
		require.Error(t, err, bad)
		_, err = homeScript(map[string]string{"axis": bad})
		require.Error(t, err, bad)
	}
}

func TestMotionLimitsReadTheConfiguration(t *testing.T) {
	t.Parallel()

	dump := func(settings map[string]string) configDump {
		out := configDump{Settings: map[string]json.RawMessage{}}
		for key, raw := range settings {
			out.Settings[key] = json.RawMessage(raw)
		}

		return out
	}

	cases := []struct {
		name string
		in   map[string]string
		want motionLimits
	}{
		{"all present", map[string]string{
			"printer":   `{"max_velocity": 300, "max_z_velocity": 15}`,
			"stepper_x": `{"position_min": -5, "position_max": 300}`,
			"stepper_y": `{"position_max": 250}`,
			"stepper_z": `{"position_min": 0, "position_max": 200}`,
		}, motionLimits{maxVelocity: 300, maxZVelocity: 15, travel: [3]float64{305, 250, 200}}},
		{"nothing", map[string]string{}, motionLimits{}},
		{"no max_z_velocity", map[string]string{"printer": `{"max_velocity": 100}`}, motionLimits{maxVelocity: 100}},
		{"inverted stepper", map[string]string{"stepper_x": `{"position_min": 10, "position_max": 5}`}, motionLimits{}},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, motionLimitsOf(dump(tc.in)), tc.name)
	}
}
