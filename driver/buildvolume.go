package moonraker

import (
	"encoding/json"
	"math"
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A Klipper machine is defined by its own configuration rather than by a model
// this driver knows, so its volume is read from there: the travel of each
// axis, position_max less position_min of the stepper_x, stepper_y and
// stepper_z sections (klippy/stepper.py). A delta machine has no such
// sections, and a section that names neither bound gives no figure; either
// way the path is not written and the volume is unknown.

// axisSections maps each volume path to the stepper section that bounds it.
//
//nolint:gochecknoglobals // a fixed table, read-only after init.
var axisSections = []struct {
	path    string
	section string
}{
	{connectorapi.BuildVolumeXPath, "stepper_x"},
	{connectorapi.BuildVolumeYPath, "stepper_y"},
	{connectorapi.BuildVolumeZPath, "stepper_z"},
}

// stepperBounds is the part of a stepper section this driver reads.
type stepperBounds struct {
	PositionMin *float64 `json:"position_min"`
	PositionMax *float64 `json:"position_max"`
}

// buildVolumePatches reads the volume out of the configuration dump.
func buildVolumePatches(dumped configDump) []connectorapi.StatePatch {
	var out []connectorapi.StatePatch

	for _, axis := range axisSections {
		var bounds stepperBounds
		if json.Unmarshal(dumped.Settings[axis.section], &bounds) != nil || bounds.PositionMax == nil {
			continue
		}

		low := 0.0
		if bounds.PositionMin != nil {
			low = *bounds.PositionMin
		}

		travel := *bounds.PositionMax - low
		if travel <= 0 || math.IsNaN(travel) || math.IsInf(travel, 0) {
			continue
		}

		out = append(out, connectorapi.StatePatch{
			Path:  axis.path,
			Value: json.RawMessage(strconv.FormatFloat(travel, 'f', -1, 64)),
		})
	}

	return out
}
