package moonraker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTheFilamentMacrosFollowTheObjectsTheMachineLists(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		available    []string
		load, unload string
	}{
		{"none", []string{"toolhead", "gcode_macro PAUSE"}, "", ""},
		{"named", []string{"gcode_macro LOAD_FILAMENT", "gcode_macro UNLOAD_FILAMENT"}, "LOAD_FILAMENT", "UNLOAD_FILAMENT"},
		{"lower case", []string{"gcode_macro load_filament", "gcode_macro unload_filament"}, "LOAD_FILAMENT", "UNLOAD_FILAMENT"},
		{"marlin", []string{"gcode_macro M701", "gcode_macro M702"}, "M701", "M702"},
		{"named wins", []string{"gcode_macro M701", "gcode_macro LOAD_FILAMENT", "gcode_macro M702", "gcode_macro UNLOAD_FILAMENT"}, "LOAD_FILAMENT", "UNLOAD_FILAMENT"},
		{"each on its own", []string{"gcode_macro LOAD_FILAMENT", "gcode_macro M702"}, "LOAD_FILAMENT", "M702"},
		{"load only", []string{"gcode_macro LOAD_FILAMENT"}, "LOAD_FILAMENT", ""},
		{"a macro whose name only starts the same", []string{"gcode_macro LOAD_FILAMENT_FAST"}, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := filamentMacrosOf(tc.available)
			assert.Equal(t, tc.load, got.load)
			assert.Equal(t, tc.unload, got.unload)
		})
	}
}

func TestFilamentVerbsTakeTheLockWaitAndNeedAKnownState(t *testing.T) {
	t.Parallel()

	assert.True(t, takesMotionLock(VerbLoadFilament))
	assert.True(t, takesMotionLock(VerbUnloadFilament))
	assert.True(t, isMotion(VerbLoadFilament))
	assert.True(t, isMotion(VerbUnloadFilament))
	assert.Equal(t, 5*time.Minute, waitFor(VerbLoadFilament, 10*time.Second))
	assert.Equal(t, 5*time.Minute, waitFor(VerbUnloadFilament, 10*time.Second))
}
