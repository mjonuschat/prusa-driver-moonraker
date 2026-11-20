package moonraker

import (
	"encoding/json"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Extruding and retracting by hand is one relative G1 move of the extruder.
// Klipper refuses it when the extruder is too cold, or when the move is longer
// or faster than the extruder's own limits, and that refusal is returned as it
// comes.
const (
	VerbMoveE        = connectorapi.Capability("core.command.move_e")
	ArgMoveEFeedrate = connectorapi.Capability("core.command.move_e#feedrate")
	ArgMoveELength   = connectorapi.Capability("core.command.move_e#length")
)

// The outer ceilings of the descriptor, which never narrow a machine, and the
// figures used when the machine's configuration does not say. The fallback
// length is Klipper's own default for max_extrude_only_distance; the fallback
// feedrate is a slow hand extrusion.
const (
	moveEFeedrateCeiling  = 60000
	extrudeLengthCeiling  = 1000
	moveEFeedrateFallback = 600
	extrudeLengthFallback = 50
)

func moveEBaseline() connectorapi.Set {
	return connectorapi.Set{
		VerbMoveE:        {Flag: true},
		ArgMoveEFeedrate: feedrateArgument(moveEFeedrateCeiling),
		ArgMoveELength: {
			Flag: true, Kind: connectorapi.KindNumber, Required: true,
			Min: -extrudeLengthCeiling, HasMin: true, Limit: extrudeLengthCeiling, HasLimit: true,
			Unit: connectorapi.UnitMillimeter,
		},
	}
}

// extruderSection is the part of the configuration's [extruder] this driver
// reads. Klipper's dump holds the default of an option it was not given.
type extruderSection struct {
	MaxExtrudeOnlyVelocity *float64 `json:"max_extrude_only_velocity"`
	MaxExtrudeOnlyDistance *float64 `json:"max_extrude_only_distance"`
}

// readExtruderLimits fills the extruder's own limits, leaving a figure the
// configuration does not give, or gives as nonsense, at zero for unknown.
func readExtruderLimits(dumped configDump, into *motionLimits) {
	var section extruderSection
	if json.Unmarshal(dumped.Settings[objectExtruder], &section) != nil {
		return
	}

	if section.MaxExtrudeOnlyVelocity != nil && finitePositive(*section.MaxExtrudeOnlyVelocity) {
		into.maxExtrudeVelocity = *section.MaxExtrudeOnlyVelocity
	}

	if section.MaxExtrudeOnlyDistance != nil && finitePositive(*section.MaxExtrudeOnlyDistance) {
		into.maxExtrudeDistance = *section.MaxExtrudeOnlyDistance
	}
}

// carryMoveE offers the verb and narrows its arguments to the extruder's limits.
func carryMoveE(into, from connectorapi.Set, limits motionLimits) {
	carry(into, from, VerbMoveE)

	if declared, present := from[ArgMoveEFeedrate]; present {
		figure := float64(moveEFeedrateFallback)
		if limits.maxExtrudeVelocity > 0 {
			figure = limits.maxExtrudeVelocity * 60
		}

		narrowLimit(&declared, figure)
		into[ArgMoveEFeedrate] = declared
	}

	if declared, present := from[ArgMoveELength]; present {
		span := float64(extrudeLengthFallback)
		if limits.maxExtrudeDistance > 0 {
			span = limits.maxExtrudeDistance
		}

		narrowLimit(&declared, span)

		if !declared.HasMin || declared.Min < -span {
			declared.Min = -span
			declared.HasMin = true
		}

		into[ArgMoveELength] = declared
	}
}

func moveEScript(args map[string]string) (string, error) {
	feedrate, err := feedrateArg(args)
	if err != nil {
		return "", err
	}

	length, ok, err := numberArg(args, "length")
	if err != nil {
		return "", err
	}

	if !ok {
		return "", malformed("", "the length argument is required")
	}

	return jogScript("G1 E" + length + " F" + feedrate), nil
}
