package moonraker

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	chamberPath       = "/temperature/chamber"
	chamberTargetPath = "/temperature/chamber_target"
	fanPathPrefix     = "/fan/"
)

// gauges turns the chamber and fan objects into patches and remembers which
// paths it has written, so a reading that stops applying removes its path
// instead of leaving a stale value.
type gauges struct {
	roles roles
	// written is every path this gauge set has put and not yet dropped.
	written map[string]struct{}
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

// appendReadings writes the chamber and the fans.
func (g *gauges) appendReadings(
	out []connectorapi.StatePatch, objects map[string]map[string]json.RawMessage,
) []connectorapi.StatePatch {
	out = g.appendChamber(out, objects)

	out = g.appendFan(out, objects, g.roles.partFan, "print")
	out = g.appendFan(out, objects, g.roles.heatbreakFan, "heatbreak")
	out = g.appendFan(out, objects, g.roles.bedFan, "bed")

	return out
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
// reports its power as the speed.
func (g *gauges) appendFan(
	out []connectorapi.StatePatch, objects map[string]map[string]json.RawMessage, object, role string,
) []connectorapi.StatePatch {
	if object == "" {
		return out
	}

	fields, ok := objects[object]
	if !ok {
		return out
	}

	speedKey := "speed"
	if strings.HasPrefix(strings.ToLower(object), "heater_generic ") {
		speedKey = "power"
	}

	out = g.number(out, fields, speedKey, fanPathPrefix+role+"/speed", true)

	return g.number(out, fields, "rpm", fanPathPrefix+role+"/rpm", false)
}
