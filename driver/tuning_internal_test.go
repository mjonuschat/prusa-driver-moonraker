package moonraker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTuningScriptsWriteThePercentageInPlainDecimal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		verb  string
		key   string
		value string
		want  string
	}{
		{"speed", "speed", "120", "M220 S120"},
		{"speed", "speed", "97.5", "M220 S97.5"},
		{"speed", "speed", "1e2", "M220 S100"},
		{"flow", "flow", "95", "M221 S95"},
		{"flow", "flow", "0.0000001", "M221 S0.0000001"},
	}

	for _, tc := range cases {
		t.Run(tc.verb+" "+tc.value, func(t *testing.T) {
			t.Parallel()

			var (
				script string
				err    error
			)

			if tc.verb == "speed" {
				script, err = speedScript(map[string]string{tc.key: tc.value})
			} else {
				script, err = flowScript(map[string]string{tc.key: tc.value})
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, script)
		})
	}
}

func TestTuningScriptsRefuseWhatTheyCannotSend(t *testing.T) {
	t.Parallel()

	for _, args := range []map[string]string{
		nil, {"speed": ""}, {"speed": "NaN"}, {"speed": "Inf"}, {"speed": "abc"},
		{"speed": "100\nM112"}, {"speed": "100;M112"},
	} {
		_, err := speedScript(args)
		require.Errorf(t, err, "speed %v", args)
	}

	_, err := flowScript(map[string]string{"flow": "100;M112"})
	require.Error(t, err)

	_, err = flowScript(nil)
	require.Error(t, err)
}

func TestSpeedAndFlowTakeTheSessionLockButAreNotMotion(t *testing.T) {
	t.Parallel()

	assert.True(t, takesMotionLock(VerbSetSpeed))
	assert.True(t, takesMotionLock(VerbSetFlow))
	assert.False(t, isMotion(VerbSetSpeed), "a speed change needs no known state")
	assert.False(t, isMotion(VerbSetFlow))
}
