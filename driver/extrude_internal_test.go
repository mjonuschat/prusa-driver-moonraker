package moonraker

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoveEIsAJogOfTheExtruder(t *testing.T) {
	t.Parallel()

	script, err := moveEScript(map[string]string{"length": "5", "feedrate": "300"})
	require.NoError(t, err)
	assert.Equal(t, "SAVE_GCODE_STATE NAME=CONNECT_JOG\nG91\nG1 E5 F300\nRESTORE_GCODE_STATE NAME=CONNECT_JOG", script)

	script, err = moveEScript(map[string]string{"length": "-2.5", "feedrate": "60"})
	require.NoError(t, err)
	assert.Contains(t, script, "G1 E-2.5 F60")
}

func TestMoveERefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()

	for _, args := range []map[string]string{
		nil,
		{"length": "5"},
		{"feedrate": "300"},
		{"length": "5", "feedrate": "0"},
		{"length": "5", "feedrate": "1.5"},
		{"length": "5", "feedrate": "abc"},
		{"length": "NaN", "feedrate": "300"},
		{"length": "5;M112", "feedrate": "300"},
		{"length": "5\nM112", "feedrate": "300"},
	} {
		_, err := moveEScript(args)
		require.Errorf(t, err, "%v", args)
	}
}

func TestMoveEIsRestoredAfterARefusalAndNeedsAKnownState(t *testing.T) {
	t.Parallel()

	assert.True(t, isJog(VerbMoveE), "a refused extrusion leaves the machine in relative mode")
	assert.True(t, isMotion(VerbMoveE))
	assert.True(t, takesMotionLock(VerbMoveE))
}

func TestTheExtruderLimitsAreReadFromTheConfiguration(t *testing.T) {
	t.Parallel()

	read := func(body string) motionLimits {
		var dumped configDump
		require.NoError(t, json.Unmarshal([]byte(body), &dumped))

		return motionLimitsOf(dumped)
	}

	got := read(`{"settings": {"extruder": {"max_extrude_only_velocity": 106.5, "max_extrude_only_distance": 151.0}}}`)
	assert.Equal(t, 106.5, got.maxExtrudeVelocity)
	assert.Equal(t, 151.0, got.maxExtrudeDistance)

	none := read(`{"settings": {}}`)
	assert.Zero(t, none.maxExtrudeVelocity)
	assert.Zero(t, none.maxExtrudeDistance)

	bad := read(`{"settings": {"extruder": {"max_extrude_only_velocity": -1, "max_extrude_only_distance": 0}}}`)
	assert.Zero(t, bad.maxExtrudeVelocity)
	assert.Zero(t, bad.maxExtrudeDistance)
}
