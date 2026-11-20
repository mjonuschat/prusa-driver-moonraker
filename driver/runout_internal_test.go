package moonraker

import (
	"encoding/json"
	"testing"
	"time"
)

const sensor = "filament_switch_sensor runout_sensor"

func runoutRun(t *testing.T, g *gauges, state, detected string) map[string]string {
	t.Helper()

	body := `{"` + sensor + `": {}}`
	if detected != "" {
		body = `{"` + sensor + `": {"filament_detected": ` + detected + `}}`
	}

	out := map[string]string{}
	for _, p := range g.appendReadings(nil, objectFields(reading(body, t)), -1, false, state) {
		out[p.Path] = string(p.Value)
	}

	return out
}

func filamentGauges() *gauges { return &gauges{roles: roles{filament: sensor}} }

var raisedDialog = map[string]string{
	"/dialog/id": "1", "/dialog/title": `"Filament runout"`,
	"/dialog/text": `"The filament sensor reports no filament."`,
}

var clearedDialog = map[string]string{"/dialog/id": "null", "/dialog/title": "null", "/dialog/text": "null"}

func TestAnEmptySensorWhileIdleRaisesNothing(t *testing.T) {
	t.Parallel()

	expect(t, runoutRun(t, filamentGauges(), "standby", "false"), map[string]string{})
}

func TestAnEmptySensorWhilePrintingRaisesTheDialog(t *testing.T) {
	t.Parallel()

	expect(t, runoutRun(t, filamentGauges(), "printing", "false"), raisedDialog)
}

func TestTheDialogClearsWhenFilamentReturns(t *testing.T) {
	t.Parallel()

	g := filamentGauges()
	runoutRun(t, g, "printing", "false")
	expect(t, runoutRun(t, g, "paused", "true"), clearedDialog)
}

func TestTheDialogClearsWhenThePrintLeavesTheBed(t *testing.T) {
	t.Parallel()

	g := filamentGauges()
	runoutRun(t, g, "printing", "false")
	expect(t, runoutRun(t, g, "cancelled", ""), clearedDialog)
}

func TestAFirstReadingDuringAParkedPrintRaisesTheDialog(t *testing.T) {
	t.Parallel()

	r := &reader{gauges: *filamentGauges()}
	out := r.patchesFrom(map[string]json.RawMessage{
		"print_stats": json.RawMessage(`{"state": "printing", "filename": "a.gcode"}`),
		sensor:        json.RawMessage(`{"filament_detected": false}`),
	}, 0, time.Now())

	got := map[string]string{}
	for _, p := range out {
		got[p.Path] = string(p.Value)
	}

	for path, value := range raisedDialog {
		if got[path] != value {
			t.Fatalf("%s is %q in %v", path, got[path], got)
		}
	}
}

func TestAReadingWithoutTheFieldKeepsTheLastValue(t *testing.T) {
	t.Parallel()

	g := filamentGauges()
	runoutRun(t, g, "standby", "false")
	expect(t, runoutRun(t, g, "printing", ""), raisedDialog)
	expect(t, runoutRun(t, g, "printing", ""), map[string]string{})
}

func TestNoFilamentRoleRaisesNothing(t *testing.T) {
	t.Parallel()

	g := &gauges{}
	expect(t, runoutRun(t, g, "printing", "false"), map[string]string{})
}

func TestADisabledSensorRaisesNothingAndReenablingRaisesIt(t *testing.T) {
	t.Parallel()

	g := filamentGauges()
	read := func(state, body string) map[string]string {
		out := map[string]string{}
		for _, p := range g.appendReadings(nil, objectFields(reading(`{"`+sensor+`": `+body+`}`, t)), -1, false, state) {
			out[p.Path] = string(p.Value)
		}

		return out
	}

	expect(t, read("printing", `{"filament_detected": false, "enabled": false}`), map[string]string{})
	expect(t, read("printing", `{"enabled": true}`), raisedDialog)
	expect(t, read("printing", `{"enabled": false}`), clearedDialog)
}
