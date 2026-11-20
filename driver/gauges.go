package moonraker

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	chamberPath       = "/temperature/chamber"
	chamberTargetPath = "/temperature/chamber_target"
	fanPathPrefix     = "/fan/"
	layerPath         = "/job/layer"
	layerCountPath    = "/job/layer_count"
)

// gauges turns the chamber and fan objects into patches and remembers which
// paths it has written, so a reading that stops applying removes its path
// instead of leaving a stale value.
type gauges struct {
	roles roles
	// written is every path this gauge set has put and not yet dropped.
	written map[string]struct{}
	// lastRPM is the rpm each mirrored role last reported as a number, so a
	// notification that names only the active head can still move the mirror.
	lastRPM map[string]json.RawMessage
	// mirror is the head each mirrored role's rpm currently sits on, absent
	// when it sits on none.
	mirror map[string]int
	runout runout
}

// mirrored maps the roles whose rpm is copied to the active head to the
// member it is written as.
var mirrored = map[string]string{
	"print":     connectorapi.PrintFanRPM,
	"heatbreak": connectorapi.HeatbreakFanRPM,
}

// numberOf reads raw as a JSON number. null is rejected explicitly because
// Unmarshal accepts it into a float64 as zero.
func numberOf(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false
	}

	var value float64
	if json.Unmarshal(raw, &value) != nil {
		return 0, false
	}

	return value, true
}

// put appends a patch and records the path as written.
func (g *gauges) put(out []connectorapi.StatePatch, path string, value json.RawMessage,
) []connectorapi.StatePatch {
	if g.written == nil {
		g.written = map[string]struct{}{}
	}

	g.written[path] = struct{}{}

	return append(out, connectorapi.StatePatch{Path: path, Value: value})
}

// drop appends a null patch for a path this gauge set wrote, and forgets it.
// A path it never wrote produces nothing.
func (g *gauges) drop(out []connectorapi.StatePatch, path string) []connectorapi.StatePatch {
	if _, ok := g.written[path]; !ok {
		return out
	}

	delete(g.written, path)

	return append(out, connectorapi.StatePatch{Path: path, Value: json.RawMessage("null")})
}

// number writes one field at a path. An absent key does nothing, a null or
// non-number value removes the path, and a number is written, as a percentage
// when scale is set.
func (g *gauges) number(
	out []connectorapi.StatePatch, fields map[string]json.RawMessage, key, path string, scale bool,
) []connectorapi.StatePatch {
	raw, present := fields[key]
	if !present {
		return out
	}

	if _, ok := numberOf(raw); !ok {
		return g.drop(out, path)
	}

	if !scale {
		return g.put(out, path, raw)
	}

	scaled := appendScaled(nil, raw, path)
	if len(scaled) == 0 {
		return g.drop(out, path)
	}

	return g.put(out, path, scaled[0].Value)
}

// appendReadings writes the chamber, the fans, the layers and the extras.
// active is the index of the active head, or -1 when none is known. onBed says
// whether a print is on the bed, which is when layers are reported.
func (g *gauges) appendReadings(
	out []connectorapi.StatePatch, objects map[string]map[string]json.RawMessage, active int, onBed bool, printState string,
) []connectorapi.StatePatch {
	out = g.appendChamber(out, objects)
	out = g.appendLayers(out, objects[objectPrintStats], onBed)
	out = g.appendExtras(out, objects)
	out = g.appendRunout(out, objects[g.roles.filament], printState)
	out = g.appendLight(out, objects)
	out = g.appendChamberFan(out, objects)

	out = g.appendFan(out, objects, g.roles.partFan, "print", active)
	out = g.appendFan(out, objects, g.roles.heatbreakFan, "heatbreak", active)
	out = g.appendFan(out, objects, g.roles.bedFan, "bed", active)

	return out
}

// appendLight writes the chamber light's level as a percentage. An output pin
// reports it as value, and an LED strip as the colours of its LEDs.
func (g *gauges) appendLight(
	out []connectorapi.StatePatch, objects map[string]map[string]json.RawMessage,
) []connectorapi.StatePatch {
	object := g.roles.light
	if object == "" {
		return out
	}

	fields := objects[object]
	if len(fields) == 0 {
		return out
	}

	if strings.HasPrefix(strings.ToLower(object), "output_pin ") {
		return g.number(out, fields, "value", lightPath, true)
	}

	fraction, ok := lightPercent(fields["color_data"])
	if !ok {
		return out
	}

	scaled := appendScaled(nil, json.RawMessage(strconv.FormatFloat(fraction, 'f', -1, 64)), lightPath)
	if len(scaled) == 0 {
		return out
	}

	return g.put(out, lightPath, scaled[0].Value)
}

// appendChamberFan writes the chamber fan's speed as a percentage.
func (g *gauges) appendChamberFan(
	out []connectorapi.StatePatch, objects map[string]map[string]json.RawMessage,
) []connectorapi.StatePatch {
	if g.roles.chamberFan == "" {
		return out
	}

	fields := objects[g.roles.chamberFan]
	if len(fields) == 0 {
		return out
	}

	return g.number(out, fields, "speed", chamberFanPath, true)
}

func (g *gauges) appendChamber(
	out []connectorapi.StatePatch, objects map[string]map[string]json.RawMessage,
) []connectorapi.StatePatch {
	if g.roles.chamber.object == "" {
		return out
	}

	fields, ok := objects[g.roles.chamber.object]
	if !ok {
		return out
	}

	out = g.number(out, fields, "temperature", chamberPath, false)

	if g.roles.chamber.heater {
		out = g.number(out, fields, "target", chamberTargetPath, false)
	}

	return out
}

// appendFan writes one fan role. A heater_generic that stands in for a fan
// reports its power as the speed. The rpm of a print or heatbreak fan is also
// mirrored to the active head.
func (g *gauges) appendFan(
	out []connectorapi.StatePatch, objects map[string]map[string]json.RawMessage, object, role string, active int,
) []connectorapi.StatePatch {
	if object == "" {
		return out
	}

	fields := objects[object]

	if len(fields) > 0 {
		out = g.fanReadings(out, fields, object, fanPathPrefix+role)
	}

	member, ok := mirrored[role]
	if !ok {
		return out
	}

	raw, reported := fields["rpm"]
	if reported {
		if _, isNumber := numberOf(raw); isNumber {
			if g.lastRPM == nil {
				g.lastRPM = map[string]json.RawMessage{}
			}

			g.lastRPM[role] = raw
		} else {
			delete(g.lastRPM, role)
		}
	}

	return g.moveMirror(out, role, member, active, reported)
}

// moveMirror keeps the member of the active head equal to the role's last rpm.
// The old head's member is dropped in the same call that writes the new one.
func (g *gauges) moveMirror(
	out []connectorapi.StatePatch, role, member string, active int, reported bool,
) []connectorapi.StatePatch {
	rpm, has := g.lastRPM[role]
	head, placed := g.mirror[role]

	if placed && (!has || active < 0 || head != active) {
		out = g.drop(out, connectorapi.ToolPath(connectorapi.HeadID(head), member))
		delete(g.mirror, role)

		placed = false
	}

	if !has || active < 0 || (placed && !reported) {
		return out
	}

	if g.mirror == nil {
		g.mirror = map[string]int{}
	}

	g.mirror[role] = active

	return g.put(out, connectorapi.ToolPath(connectorapi.HeadID(active), member), rpm)
}

// dropLayers removes both layer paths.
func (g *gauges) dropLayers(out []connectorapi.StatePatch) []connectorapi.StatePatch {
	out = g.drop(out, layerPath)

	return g.drop(out, layerCountPath)
}

func (g *gauges) fanReadings(
	out []connectorapi.StatePatch, fields map[string]json.RawMessage, object, base string,
) []connectorapi.StatePatch {
	speedKey := "speed"
	if strings.HasPrefix(strings.ToLower(object), "heater_generic ") {
		speedKey = "power"
	}

	out = g.number(out, fields, speedKey, base+"/speed", true)

	return g.number(out, fields, "rpm", base+"/rpm", false)
}

func (g *gauges) appendExtras(
	out []connectorapi.StatePatch, objects map[string]map[string]json.RawMessage,
) []connectorapi.StatePatch {
	for _, object := range g.roles.extraSensors {
		if fields, ok := objects[object]; ok {
			out = g.number(out, fields, "temperature", "/sensors/"+heaterName(object)+"/temperature", false)
		}
	}

	for _, object := range g.roles.extraFans {
		if fields, ok := objects[object]; ok {
			out = g.fanReadings(out, fields, object, "/fans/"+heaterName(object))
		}
	}

	return out
}

// appendLayers writes the layer numbers from print_stats.info while a print is
// on the bed. Moonraker sends only what changed, so an info that is omitted,
// or an object that omits a key, leaves the last value. An info that is null
// has no values to inspect and removes both paths.
func (g *gauges) appendLayers(
	out []connectorapi.StatePatch, stats map[string]json.RawMessage, onBed bool,
) []connectorapi.StatePatch {
	raw, present := stats["info"]

	if !onBed {
		return g.dropLayers(out)
	}

	if !present {
		return out
	}

	var info map[string]json.RawMessage
	if json.Unmarshal(raw, &info) != nil || info == nil {
		return g.dropLayers(out)
	}

	out = g.number(out, info, "current_layer", layerPath, false)

	return g.number(out, info, "total_layer", layerCountPath, false)
}
