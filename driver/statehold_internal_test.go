package moonraker

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stateAfter feeds one print state and one webhooks state, either empty to
// leave it unreported, and returns the /state word written, or "" for none.
func stateAfter(t *testing.T, m *machineState, print, hook string) string {
	t.Helper()

	raw := func(word string) json.RawMessage {
		if word == "" {
			return nil
		}

		return json.RawMessage(`"` + word + `"`)
	}

	patches := m.appendState(nil, raw(print), raw(hook))
	if len(patches) == 0 {
		return ""
	}

	var word string
	assert.NoError(t, json.Unmarshal(patches[0].Value, &word))

	return word
}

func TestAnEndedPrintKeepsItsEndWordWhileKlipperReportsStandby(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ klipper, word string }{
		{"cancelled", "STOPPED"},
		{"complete", "FINISHED"},
	} {
		m := &machineState{}

		assert.Equal(t, "PRINTING", stateAfter(t, m, "printing", ""))
		assert.Equal(t, tc.word, stateAfter(t, m, tc.klipper, ""))
		assert.Equal(t, tc.word, stateAfter(t, m, "standby", ""), "a reset after %s is not idle", tc.klipper)
		assert.Equal(t, tc.word, stateAfter(t, m, "standby", ""))
	}
}

func TestANewPrintEndsTheHold(t *testing.T) {
	t.Parallel()

	m := &machineState{}
	stateAfter(t, m, "cancelled", "")
	assert.Equal(t, "STOPPED", stateAfter(t, m, "standby", ""))
	assert.Equal(t, "PRINTING", stateAfter(t, m, "printing", ""))
	assert.Equal(t, "IDLE", stateAfter(t, m, "standby", ""), "a print that ends in standby with no end word is idle")
}

func TestAMachineThatNeverPrintedIsIdle(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "IDLE", stateAfter(t, &machineState{}, "standby", ""))
}

func TestAPrintErrorIsNotHeld(t *testing.T) {
	t.Parallel()

	// A held ERROR would refuse start_print until the next restart.
	m := &machineState{}
	assert.Equal(t, "ERROR", stateAfter(t, m, "error", ""))
	assert.Equal(t, "IDLE", stateAfter(t, m, "standby", ""))
}

func TestAShutdownEndsTheHold(t *testing.T) {
	t.Parallel()

	m := &machineState{}
	stateAfter(t, m, "cancelled", "")
	stateAfter(t, m, "standby", "")
	assert.Equal(t, "ERROR", stateAfter(t, m, "", "shutdown"))
	assert.Equal(t, "IDLE", stateAfter(t, m, "standby", "ready"))
}

func TestAWebhooksReadingAloneKeepsTheHold(t *testing.T) {
	t.Parallel()

	m := &machineState{}
	stateAfter(t, m, "complete", "")
	stateAfter(t, m, "standby", "")
	assert.Equal(t, "FINISHED", stateAfter(t, m, "", "ready"))
}
