package moonraker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTheLevellingCommandFollowsTheObjectsTheMachineLists(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		available []string
		command   string
		both      bool
	}{
		{"neither", []string{"toolhead", "extruder"}, "", false},
		{"z_tilt", []string{"toolhead", "z_tilt"}, "Z_TILT_ADJUST", false},
		{"quad_gantry_level", []string{"toolhead", "quad_gantry_level"}, "QUAD_GANTRY_LEVEL", false},
		{"both prefer z_tilt", []string{"quad_gantry_level", "z_tilt"}, "Z_TILT_ADJUST", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			command, both := levelCommandOf(tc.available)
			assert.Equal(t, tc.command, command)
			assert.Equal(t, tc.both, both)
		})
	}
}

func TestSlowVerbsWaitLongerThanOneCall(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 10*time.Second, waitFor(VerbMove, 10*time.Second))
	assert.Equal(t, 10*time.Second, waitFor(VerbSendGCode, 10*time.Second))
	assert.Equal(t, 60*time.Second, waitFor(VerbHome, 10*time.Second))
	assert.Equal(t, 5*time.Minute, waitFor(VerbMeshBedLeveling, 10*time.Second))
	// A configured timeout longer than the verb's own wait is kept.
	assert.Equal(t, 10*time.Minute, waitFor(VerbHome, 10*time.Minute))
}
