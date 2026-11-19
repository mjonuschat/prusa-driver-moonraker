package moonraker

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A reading of an idle machine must write no print path at all.
//
// Klipper reports print_stats on every frame, printing or not: an idle machine
// sends state "standby", an empty filename and a print_duration of zero. Those
// three are not a print, and the driver contract asks every driver that reports
// a print to report one at /job/id, because the host names the print's row and
// its events by that id. A reading that writes /job/name and /job/progress with
// no /job/id beside them gives the host a print it cannot name.
func TestAnIdleMachineReportsNoPrintPath(t *testing.T) {
	t.Parallel()

	held := &reader{}
	patches := held.patchesFrom(reading(`{
		"print_stats": {"state": "standby", "filename": "", "print_duration": 0, "total_duration": 0},
		"display_status": {"progress": 0}
	}`, t), 0, time.Unix(0, 0))

	for _, patch := range patches {
		if strings.HasPrefix(patch.Path, "/job/") {
			t.Errorf("an idle machine wrote %s, and no print is on the bed", patch.Path)
		}
	}
}

// A print on the bed reports its id and the rest of its paths together.
func TestAPrintOnTheBedReportsItsIDAndItsOtherPaths(t *testing.T) {
	t.Parallel()

	held := &reader{}
	patches := held.patchesFrom(reading(`{
		"print_stats": {
			"state": "printing", "filename": "bracket.gcode",
			"print_duration": 61.5, "filament_used": 120.25, "total_duration": 75
		},
		"display_status": {"progress": 0.42}
	}`, t), 0, time.Unix(0, 0))

	written := map[string]bool{}
	for _, patch := range patches {
		written[patch.Path] = true
	}

	for _, path := range []string{
		jobIDPath, jobNamePath, jobTimePrintingPath, jobFilamentPath, jobProgressPath,
	} {
		if !written[path] {
			t.Errorf("a print on the bed wrote no %s", path)
		}
	}
}

// The last reading of a print that ended still names the print it is about.
//
// The outcome and the file belong to the print that just came off the bed, so
// they are written together. Dropping the name here would leave the host's tree
// holding the previous print's name against this print's outcome.
func TestTheReadingThatEndsAPrintStillNamesIt(t *testing.T) {
	t.Parallel()

	held := &reader{}
	held.patchesFrom(reading(`{
		"print_stats": {
			"state": "printing", "filename": "bracket.gcode",
			"print_duration": 61.5, "total_duration": 75
		}
	}`, t), 0, time.Unix(0, 0))

	patches := held.patchesFrom(reading(`{
		"print_stats": {
			"state": "complete", "filename": "bracket.gcode",
			"print_duration": 120, "total_duration": 130
		}
	}`, t), 0, time.Unix(0, 0))

	written := map[string]bool{}
	for _, patch := range patches {
		written[patch.Path] = true
	}

	if !written[jobOutcomePath] {
		t.Error("the print ended and no outcome was written")
	}

	if !written[jobNamePath] {
		t.Error("the print ended and its name was not written with the outcome")
	}
}

// reading turns one JSON object into the shape a Moonraker status frame
// carries, so a case above reads as the frame the machine sends.
func reading(body string, t *testing.T) map[string]json.RawMessage {
	t.Helper()

	var status map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatalf("the case is not a status object: %v", err)
	}

	return status
}

// An extruder object is a head counted from 0, with no upper bound, and an
// object that only looks like one is not.
func TestToolOfReadsKlippersExtruderNames(t *testing.T) {
	t.Parallel()

	for object, want := range map[string]int{
		"extruder": 0, "extruder1": 1, "extruder4": 4,
		"extruder0": -1, "extruder01": -1, "extruder_stepper": -1, "heater_bed": -1, "extruder99": 99, "extruder-1": -1,
	} {
		got, ok := toolOf(object)
		if want < 0 && ok {
			t.Errorf("%s is tool %d, want no tool", object, got)
		}

		if want >= 0 && (!ok || got != want) {
			t.Errorf("%s is tool %d, want %d", object, got, want)
		}
	}
}
func printingOf(file string, duration string, t *testing.T) map[string]json.RawMessage {
	t.Helper()

	return reading(`{"print_stats": {"state": "printing", "filename": "`+file+
		`"`+duration+`}}`, t)
}

func jobIDOf(patches []connectorapi.StatePatch) string {
	for _, patch := range patches {
		if patch.Path == jobIDPath {
			return klipperText(patch.Value)
		}
	}

	return ""
}

func idAt(t *testing.T, file string, eventTime float64, now time.Time) string {
	t.Helper()

	return jobIDOf((&reader{}).patchesFrom(printingOf(file, `, "total_duration": 75`, t), eventTime, now))
}

func TestAPrintKeepsItsIDWhateverTheDriversClockSays(t *testing.T) {
	t.Parallel()

	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	first := idAt(t, "bracket.gcode", 1000.25, base.Add(-100*time.Millisecond))
	second := idAt(t, "bracket.gcode", 1000.25, base.Add(100*time.Millisecond))

	if first == "" || first != second {
		t.Errorf("ids %q and %q, want one", first, second)
	}
}

func TestAnotherStartIsAnotherPrint(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)
	if idAt(t, "bracket.gcode", 1000.25, now) == idAt(t, "bracket.gcode", 1030.25, now) {
		t.Error("two starts shared an id")
	}
}

func TestFloatNoiseDoesNotChangeThePrint(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)
	if idAt(t, "bracket.gcode", 1000.25, now) != idAt(t, "bracket.gcode", 1000.25+1e-9, now) {
		t.Error("float noise changed the id")
	}
}

func TestAPrintFirstSeenWithoutTotalDurationGetsADifferentIDFromAReconnect(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)
	bare := jobIDOf((&reader{}).patchesFrom(printingOf("bracket.gcode", ``, t), 1000.25, now))
	both := idAt(t, "bracket.gcode", 1000.25, now)

	if bare == "" || both == "" || bare == both {
		t.Errorf("ids %q and %q, want two non-empty and different", bare, both)
	}
}

func TestAnotherFileIsAnotherPrint(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)
	if idAt(t, "bracket.gcode", 1000.25, now) == idAt(t, "bolt.gcode", 1000.25, now) {
		t.Error("two files shared an id")
	}
}

func TestAFileChangeInOneSessionIsAnotherPrint(t *testing.T) {
	t.Parallel()

	held := &reader{}
	now := time.Unix(0, 0)
	first := jobIDOf(held.patchesFrom(printingOf("bracket.gcode", `, "total_duration": 75`, t), 1000, now))
	second := jobIDOf(held.patchesFrom(printingOf("bolt.gcode", `, "total_duration": 75`, t), 1000, now))

	if first == "" || second == "" || first == second {
		t.Errorf("ids %q and %q, want two different", first, second)
	}
}

func TestAReadingWithNoKlipperClockUsesTheDriversClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 1, 1, 10, 0, 30, 0, time.UTC)
	want := idAt(t, "bracket.gcode", 0, now)

	if want == "" {
		t.Fatal("no id with no clock")
	}

	if got := idAt(t, "bracket.gcode", -5, now); got != want {
		t.Errorf("eventTime -5 gave %q, want %q", got, want)
	}

	bare := jobIDOf((&reader{}).patchesFrom(printingOf("bracket.gcode", ``, t), 0, now))
	held := &reader{}
	if got := jobIDOf(held.patchesFrom(printingOf("bracket.gcode", ``, t), 1000, now)); got != bare {
		t.Errorf("no total_duration gave %q, want %q", got, bare)
	}

	if got := jobIDOf(held.patchesFrom(printingOf("bracket.gcode", ``, t), 1000, now)); got != bare {
		t.Errorf("a second reading gave %q, want %q", got, bare)
	}
}

func TestAFilenameOnlyNotificationMidPrintIsANewPrint(t *testing.T) {
	t.Parallel()

	held := &reader{}
	now := time.Unix(0, 0)
	first := jobIDOf(held.patchesFrom(printingOf("bracket.gcode", `, "total_duration": 75`, t), 1000, now))
	second := jobIDOf(held.patchesFrom(reading(`{"print_stats":{"filename":"bolt.gcode"}}`, t), 1000, now))

	if second == "" || second == first {
		t.Errorf("ids %q then %q, want a new one", first, second)
	}

	third := jobIDOf(held.patchesFrom(reading(`{"print_stats":{"print_duration":5}}`, t), 1000, now))
	if third != "" || held.prints.id == "" {
		t.Errorf("a duration changed the id to %q, held %q", third, held.prints.id)
	}
}

func TestAReprintOfTheSameFileKeepsItsFileName(t *testing.T) {
	t.Parallel()

	held := &reader{}
	now := time.Unix(0, 0)
	first := jobIDOf(held.patchesFrom(printingOf("bracket.gcode", `, "total_duration": 75`, t), 1000, now))
	held.patchesFrom(reading(`{"print_stats":{"state":"complete"}}`, t), 1100, now)
	held.patchesFrom(reading(`{"print_stats":{"state":"standby"}}`, t), 1200, now)
	last := jobIDOf(held.patchesFrom(
		reading(`{"print_stats":{"state":"printing","total_duration":0.3}}`, t), 1600, now))
	want := jobIDOf((&reader{}).patchesFrom(
		printingOf("bracket.gcode", `, "total_duration": 0.3`, t), 1600, now))

	if last == "" || last == first || last != want {
		t.Errorf("reprint id %q, first %q, fresh %q", last, first, want)
	}
}

func TestAStateBeforeTheFileMintsNoID(t *testing.T) {
	t.Parallel()

	held := &reader{}
	now := time.Unix(0, 0)

	for _, patch := range held.patchesFrom(
		reading(`{"print_stats":{"state":"printing","total_duration":0.2}}`, t), 1000, now) {
		if strings.HasPrefix(patch.Path, "/job/") {
			t.Errorf("wrote %s before the file was known", patch.Path)
		}
	}

	id := jobIDOf(held.patchesFrom(reading(`{"print_stats":{"filename":"bracket.gcode"}}`, t), 1000, now))
	if id == "" || id == derivedJobID("", jobStart(nil, 0, now)) {
		t.Errorf("minted %q", id)
	}

	if again := jobIDOf(held.patchesFrom(reading(`{"print_stats":{"print_duration":1}}`, t), 1000, now)); (again != "" && again != id) ||
		held.prints.id != id {
		t.Errorf("the id moved to %q, held %q", again, held.prints.id)
	}
}

func TestAStartSplitAcrossTwoNotificationsKeepsItsFile(t *testing.T) {
	t.Parallel()

	held := &reader{}
	now := time.Unix(0, 0)

	if id := jobIDOf(held.patchesFrom(reading(`{"print_stats":{"filename":"bracket.gcode"}}`, t), 1000, now)); id != "" {
		t.Errorf("an idle file wrote id %q", id)
	}

	id := jobIDOf(held.patchesFrom(
		reading(`{"print_stats":{"state":"printing","total_duration":0.2}}`, t), 1000, now))
	want := jobIDOf((&reader{}).patchesFrom(
		printingOf("bracket.gcode", `, "total_duration": 0.2`, t), 1000, now))

	if id == "" || id != want {
		t.Errorf("id %q, want %q", id, want)
	}

	if again := jobIDOf(held.patchesFrom(
		reading(`{"print_stats":{"filename":"bracket.gcode","print_duration":1}}`, t), 1000, now)); (again != "" && again != id) ||
		held.prints.id != id {
		t.Errorf("the id moved to %q, held %q", again, held.prints.id)
	}
}
