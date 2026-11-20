package moonraker

import (
	"encoding/json"
	"testing"
)

func fanRoles() roles {
	return roles{partFan: "fan", bedFan: "heater_generic bed_fans",
		chamber: chamberRole{object: "temperature_sensor Chamber"}}
}

func run(t *testing.T, g *gauges, body string) map[string]string {
	t.Helper()

	return runOn(t, g, -1, body)
}

func runOn(t *testing.T, g *gauges, active int, body string) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, p := range g.appendReadings(nil, objectFields(reading(body, t)), active, false, "") {
		out[p.Path] = string(p.Value)
	}

	return out
}

func expect(t *testing.T, got map[string]string, want map[string]string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	for k, v := range want {
		if got[k] != v {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestAChamberSensorWritesOnlyItsTemperature(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: fanRoles()}
	expect(t, run(t, g, `{"temperature_sensor Chamber": {"temperature": 26.4, "target": 5}}`),
		map[string]string{chamberPath: "26.4"})
}

func TestAChamberHeaterWritesItsTemperatureAndTarget(t *testing.T) {
	t.Parallel()

	r := roles{chamber: chamberRole{object: "heater_generic chamber", heater: true}}
	g := &gauges{roles: r}
	expect(t, run(t, g, `{"heater_generic chamber": {"temperature": 30, "target": 0}}`),
		map[string]string{chamberPath: "30", chamberTargetPath: "0"})
}

func TestAFanReportsItsSpeedAsAPercentage(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: fanRoles()}
	expect(t, run(t, g, `{"fan": {"speed": 0.5}}`), map[string]string{"/fan/print/speed": "50"})
}

func TestARealRPMIsWrittenAndZeroCounts(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: fanRoles()}
	expect(t, run(t, g, `{"fan": {"rpm": 0.0}}`), map[string]string{"/fan/print/rpm": "0.0"})
}

func TestNumberOfRejectsNullAndNonNumbers(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"null", `"3"`, "true", "[]", "", " null ", "\tnull\n"} {
		if _, ok := numberOf(json.RawMessage(bad)); ok {
			t.Errorf("%q was taken for a number", bad)
		}
	}

	for _, good := range []string{"0", "0.0", "3158.5"} {
		if _, ok := numberOf(json.RawMessage(good)); !ok {
			t.Errorf("%q was not taken for a number", good)
		}
	}
}

func TestANullRPMWritesNoPathAndRemovesAWrittenOne(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: fanRoles()}
	expect(t, run(t, g, `{"fan": {"rpm": 3158.5}}`), map[string]string{"/fan/print/rpm": "3158.5"})
	expect(t, run(t, g, `{"fan": {"rpm": null}}`), map[string]string{"/fan/print/rpm": "null"})
	expect(t, run(t, g, `{"fan": {"rpm": null}}`), map[string]string{})
}

func TestAHeaterDemotedToAFanReportsItsPowerAsTheSpeed(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: roles{bedFan: "heater_generic bed_fans"}}
	expect(t, run(t, g, `{"heater_generic bed_fans": {"power": 1.0, "temperature": 20}}`),
		map[string]string{"/fan/bed/speed": "100"})
}

func TestAReadingWithoutTheObjectWritesNothing(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: fanRoles()}
	run(t, g, `{"fan": {"speed": 0.5, "rpm": 100}}`)
	expect(t, run(t, g, `{"extruder": {"temperature": 200}}`), map[string]string{})
}

func TestANullSpeedRemovesAWrittenSpeedAndAnOmittedOneKeepsIt(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: fanRoles()}
	run(t, g, `{"fan": {"speed": 0.5}}`)
	expect(t, run(t, g, `{"fan": {"speed": null}}`), map[string]string{"/fan/print/speed": "null"})
	expect(t, run(t, g, `{"fan": {"rpm": null}}`), map[string]string{})
}

func TestANullPowerOnADemotedHeaterRemovesTheBedFanSpeed(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: roles{bedFan: "heater_generic bed_fans"}}
	run(t, g, `{"heater_generic bed_fans": {"power": 0.5}}`)
	expect(t, run(t, g, `{"heater_generic bed_fans": {"power": null}}`),
		map[string]string{"/fan/bed/speed": "null"})
}

func TestANullChamberTemperatureOrTargetRemovesItsPath(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: roles{chamber: chamberRole{object: "heater_generic chamber", heater: true}}}
	run(t, g, `{"heater_generic chamber": {"temperature": 30, "target": 40}}`)
	expect(t, run(t, g, `{"heater_generic chamber": {"temperature": null, "target": null}}`),
		map[string]string{chamberPath: "null", chamberTargetPath: "null"})
}

func headRoles() roles {
	return roles{partFan: "fan", heatbreakFan: "heater_fan hb", bedFan: "heater_generic bed_fans"}
}

func TestAnRPMIsMirroredToTheActiveHead(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: headRoles()}
	expect(t, runOn(t, g, 0, `{"fan": {"rpm": 3000}}`),
		map[string]string{"/fan/print/rpm": "3000", "/tools/0/print_fan_rpm": "3000"})
}

func TestTheMirrorMovesWithTheActiveHeadAndLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: headRoles()}
	runOn(t, g, 0, `{"fan": {"rpm": 3000}}`)
	expect(t, runOn(t, g, 1, `{}`),
		map[string]string{"/tools/0/print_fan_rpm": "null", "/tools/1/print_fan_rpm": "3000"})
}

func TestNoKnownHeadDropsTheMirror(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: headRoles()}
	runOn(t, g, 0, `{"fan": {"rpm": 3000}}`)
	expect(t, runOn(t, g, -1, `{}`), map[string]string{"/tools/0/print_fan_rpm": "null"})
}

func TestANullRPMDropsTheMirrorToo(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: headRoles()}
	runOn(t, g, 0, `{"fan": {"rpm": 3000}}`)
	expect(t, runOn(t, g, 0, `{"fan": {"rpm": null}}`),
		map[string]string{"/fan/print/rpm": "null", "/tools/0/print_fan_rpm": "null"})
}

func TestTheBedFanHasNoMirror(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: headRoles()}
	expect(t, runOn(t, g, 0, `{"heater_generic bed_fans": {"power": 0.5, "rpm": 900}}`),
		map[string]string{"/fan/bed/speed": "50", "/fan/bed/rpm": "900"})
}

func layersOn(t *testing.T, g *gauges, onBed bool, body string) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, p := range g.appendReadings(nil, objectFields(reading(body, t)), -1, onBed, "") {
		out[p.Path] = string(p.Value)
	}

	return out
}

const lele = `{"print_stats": {"info": {"current_layer": 19, "total_layer": 57}}}`

func TestLayersAreReportedOnlyWhileAPrintIsOnTheBed(t *testing.T) {
	t.Parallel()

	g := &gauges{}
	expect(t, layersOn(t, g, true, lele), map[string]string{layerPath: "19", layerCountPath: "57"})
	expect(t, layersOn(t, &gauges{}, false, lele), map[string]string{})
	expect(t, layersOn(t, &gauges{}, true, `{"print_stats": {"info": null}}`), map[string]string{})
}

func TestLayersAreRemovedWhenThePrintLeavesTheBed(t *testing.T) {
	t.Parallel()

	g := &gauges{}
	layersOn(t, g, true, lele)
	expect(t, layersOn(t, g, false, `{"print_stats": {"state": "standby"}}`),
		map[string]string{layerPath: "null", layerCountPath: "null"})
}

func TestANullLayerAfterANumberRemovesIt(t *testing.T) {
	t.Parallel()

	g := &gauges{}
	layersOn(t, g, true, lele)
	expect(t, layersOn(t, g, true, `{"print_stats": {"info": {"current_layer": null}}}`),
		map[string]string{layerPath: "null"})
}

func TestANullInfoObjectRemovesBothLayerPaths(t *testing.T) {
	t.Parallel()

	g := &gauges{}
	layersOn(t, g, true, lele)
	expect(t, layersOn(t, g, true, `{"print_stats": {"info": null}}`),
		map[string]string{layerPath: "null", layerCountPath: "null"})
}

func TestAnOmittedInfoKeepsTheLayers(t *testing.T) {
	t.Parallel()

	g := &gauges{}
	layersOn(t, g, true, lele)
	expect(t, layersOn(t, g, true, `{"print_stats": {"print_duration": 5}}`), map[string]string{})
}

func TestAnInfoThatOmitsOneKeyKeepsThatLayer(t *testing.T) {
	t.Parallel()

	g := &gauges{}
	layersOn(t, g, true, lele)
	expect(t, layersOn(t, g, true, `{"print_stats": {"info": {"current_layer": 20}}}`),
		map[string]string{layerPath: "20"})
}

func extraRoles() roles {
	return roles{
		extraSensors: []string{"temperature_sensor Electrical_Cabinet"},
		extraFans:    []string{"controller_fan controller_fan"},
	}
}

func TestAnExtraSensorPublishesUnderItsOwnName(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: extraRoles()}
	expect(t, run(t, g, `{"temperature_sensor Electrical_Cabinet": {"temperature": 31.2}}`),
		map[string]string{"/sensors/Electrical_Cabinet/temperature": "31.2"})
}

func TestAnExtraFanPublishesSpeedAndRPMUnderItsOwnName(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: extraRoles()}
	expect(t, run(t, g, `{"controller_fan controller_fan": {"speed": 0.5, "rpm": 3158.5}}`),
		map[string]string{"/fans/controller_fan/speed": "50", "/fans/controller_fan/rpm": "3158.5"})
}

func TestAnExtraThatTheMachineDoesNotListPublishesNothing(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: extraRoles()}
	expect(t, run(t, g, `{"temperature_sensor Other": {"temperature": 1}, "fan_generic x": {"speed": 1}}`),
		map[string]string{})
}

func TestANullExtraSensorTemperatureOrFanSpeedRemovesItsPath(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: extraRoles()}
	run(t, g, `{"temperature_sensor Electrical_Cabinet": {"temperature": 31.2}, "controller_fan controller_fan": {"speed": 1}}`)
	expect(t, run(t, g, `{"temperature_sensor Electrical_Cabinet": {"temperature": null}, "controller_fan controller_fan": {"speed": null}}`),
		map[string]string{"/sensors/Electrical_Cabinet/temperature": "null", "/fans/controller_fan/speed": "null"})
}
