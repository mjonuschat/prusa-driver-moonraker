package moonraker

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOlderMetadataReplyDoesNotReplaceCurrentEstimate(t *testing.T) {
	host, err := moonrakersim.Start(t.Context(), moonrakersim.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = host.Close() })

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+host.Address()+"/websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	s := newSession(conn, time.Second, liveness{}.orShipping(), slog.New(slog.DiscardHandler))
	t.Cleanup(s.stopSpool)

	s.reader.prints.id, s.reader.prints.file = "a", "a.gcode"
	s.askEstimate()
	first := s.nextID.Load()
	s.reader.prints.id, s.reader.prints.file = "b", "b.gcode"
	s.askEstimate()
	second := s.nextID.Load()

	require.True(t, s.answerRequest(second, rpcFrame{Result: json.RawMessage(`{"estimated_time":200}`)}))
	require.True(t, s.answerRequest(first, rpcFrame{Result: json.RawMessage(`{"estimated_time":100}`)}))
	require.Equal(t, "b.gcode", s.reader.prints.estimateFile)
	require.Equal(t, 200.0, s.reader.prints.estimate)
}

// remainingIn is the /job/time_remaining value among the patches, or "" for none.
func remainingIn(patches []connectorapi.StatePatch) string {
	var value string
	for _, patch := range patches {
		if patch.Path == jobTimeRemainingPath {
			value = string(patch.Value)
		}
	}

	return value
}

func printingFrame(extra string) string {
	return `{"print_stats": {"state": "printing", "filename": "bracket.gcode", "print_duration": 400}` + extra + `}`
}

// readerWithEstimate is a reader that has been told how long bracket.gcode takes.
func readerWithEstimate(seconds float64) *reader {
	held := &reader{}
	held.prints.estimate = seconds
	held.prints.estimateFile = "bracket.gcode"

	return held
}

func TestTimeRemainingIsTheEstimateScaledByProgress(t *testing.T) {
	t.Parallel()

	held := readerWithEstimate(28064)
	patches := held.patchesFrom(reading(
		printingFrame(`, "display_status": {"progress": 0.01165}`), t), 0, time.Unix(0, 0))

	assert.Equal(t, "27737", remainingIn(patches))
}

func TestTimeRemainingFallsBackToTheEstimateLessThePrintDuration(t *testing.T) {
	t.Parallel()

	held := readerWithEstimate(1000)
	patches := held.patchesFrom(reading(printingFrame(""), t), 0, time.Unix(0, 0))

	assert.Equal(t, "600", remainingIn(patches))
}

func TestTimeRemainingNeedsAnEstimateForThisFile(t *testing.T) {
	t.Parallel()

	none := &reader{}
	assert.Empty(t, remainingIn(none.patchesFrom(reading(printingFrame(""), t), 0, time.Unix(0, 0))))

	other := readerWithEstimate(1000)
	other.prints.estimateFile = "another.gcode"
	assert.Empty(t, remainingIn(other.patchesFrom(reading(printingFrame(""), t), 0, time.Unix(0, 0))))
}

func TestTimeRemainingNeverGoesBelowZero(t *testing.T) {
	t.Parallel()

	done := readerWithEstimate(1000)
	assert.Equal(t, "0", remainingIn(done.patchesFrom(reading(
		printingFrame(`, "display_status": {"progress": 1.0}`), t), 0, time.Unix(0, 0))))

	over := readerWithEstimate(300)
	assert.Equal(t, "0", remainingIn(over.patchesFrom(reading(printingFrame(""), t), 0, time.Unix(0, 0))))
}

func TestTimeRemainingIsWrittenOnlyWhenItChanges(t *testing.T) {
	t.Parallel()

	held := readerWithEstimate(1000)
	frame := printingFrame(`, "display_status": {"progress": 0.5}`)

	assert.Equal(t, "500", remainingIn(held.patchesFrom(reading(frame, t), 0, time.Unix(0, 0))))
	assert.Empty(t, remainingIn(held.patchesFrom(reading(frame, t), 0, time.Unix(0, 0))))
	assert.Equal(t, "250", remainingIn(held.patchesFrom(reading(
		`{"display_status": {"progress": 0.75}}`, t), 0, time.Unix(0, 0))), "a frame without print_stats still counts")
}

func TestTimeRemainingStopsWhenThePrintEnds(t *testing.T) {
	t.Parallel()

	held := readerWithEstimate(1000)
	held.patchesFrom(reading(printingFrame(""), t), 0, time.Unix(0, 0))

	ended := held.patchesFrom(reading(
		`{"print_stats": {"state": "complete", "filename": "bracket.gcode", "print_duration": 900}}`, t),
		0, time.Unix(0, 0))
	assert.Empty(t, remainingIn(ended))
}

func TestANewPrintForgetsTheLastProgress(t *testing.T) {
	t.Parallel()

	held := readerWithEstimate(1000)
	held.patchesFrom(reading(printingFrame(`, "display_status": {"progress": 0.9}`), t), 0, time.Unix(0, 0))
	held.patchesFrom(reading(`{"print_stats": {"state": "complete", "filename": "bracket.gcode"}}`, t), 0, time.Unix(0, 0))
	held.patchesFrom(reading(`{"print_stats": {"state": "standby", "filename": ""}}`, t), 0, time.Unix(0, 0))

	again := held.patchesFrom(reading(
		`{"print_stats": {"state": "printing", "filename": "bracket.gcode", "print_duration": 10}}`, t),
		600, time.Unix(0, 0))
	assert.Equal(t, "990", remainingIn(again), "the old 90 percent must not shorten the new print")
}

func TestNewPrintDoesNotUsePreviousDuration(t *testing.T) {
	held := readerWithEstimate(1000)
	held.patchesFrom(reading(printingFrame(""), t), 0, time.Unix(0, 0))
	held.patchesFrom(reading(`{"print_stats":{"state":"complete"}}`, t), 0, time.Unix(0, 0))
	opening := held.patchesFrom(reading(`{"print_stats":{"state":"printing"}}`, t), 100, time.Unix(0, 0))
	require.Equal(t, "null", remainingIn(opening), "the new print has no duration yet")
	updated := held.patchesFrom(reading(`{"print_stats":{"print_duration":10}}`, t), 101, time.Unix(0, 0))
	require.Equal(t, "990", remainingIn(updated))
}

func TestNewFileClearsPreviousRemainingTimeUntilItsEstimateIsKnown(t *testing.T) {
	held := readerWithEstimate(1000)
	held.patchesFrom(reading(printingFrame(`, "display_status": {"progress": 0.5}`), t), 0, time.Unix(0, 0))

	patches := held.patchesFrom(reading(
		`{"print_stats": {"state": "printing", "filename": "other.gcode", "print_duration": 1}}`, t),
		100, time.Unix(0, 0))
	require.Equal(t, "null", remainingIn(patches))
}

func TestTheEstimateIsAskedForOncePerFile(t *testing.T) {
	t.Parallel()

	held := &reader{}

	_, wanted := held.prints.wantEstimate()
	assert.False(t, wanted, "nothing is printing")

	held.patchesFrom(reading(printingFrame(""), t), 0, time.Unix(0, 0))

	file, wanted := held.prints.wantEstimate()
	require.True(t, wanted)
	assert.Equal(t, "bracket.gcode", file)

	_, wanted = held.prints.wantEstimate()
	assert.False(t, wanted, "it is not asked for again")
}
