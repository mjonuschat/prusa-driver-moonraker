package moonraker

import (
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The speed and flow overrides of the Controls tab. Klipper keeps both as
// factors of gcode_move, which the driver already reads for the two chips.
const (
	VerbSetSpeed = connectorapi.Capability("core.command.set_speed")
	ArgSpeed     = connectorapi.Capability("core.command.set_speed#speed")
	VerbSetFlow  = connectorapi.Capability("core.command.set_flow")
	ArgFlow      = connectorapi.Capability("core.command.set_flow#flow")
)

// The bounds Prusa's own firmware verbs declare for the same two arguments, in
// percent. The panel stays inside 50 to 200 by itself.
const (
	tuningFloor   = 20
	tuningCeiling = 999
)

// tuningArgument is one override's descriptor. It is a number and not an
// integer because M220 and M221 take a decimal; the panel sends whole numbers.
func tuningArgument() connectorapi.Value {
	return connectorapi.Value{
		Flag: true, Kind: connectorapi.KindNumber, Required: true,
		Min: tuningFloor, HasMin: true, Limit: tuningCeiling, HasLimit: true,
		Unit: connectorapi.UnitPercent,
	}
}

func tuningBaseline() connectorapi.Set {
	return connectorapi.Set{
		VerbSetSpeed: {Flag: true},
		ArgSpeed:     tuningArgument(),
		VerbSetFlow:  {Flag: true},
		ArgFlow:      tuningArgument(),
	}
}

// carryTuning offers the two verbs when the machine lists gcode_move.
func carryTuning(into, from connectorapi.Set, available []string) {
	if slices.Contains(available, objectGcodeMove) {
		carry(into, from, VerbSetSpeed, ArgSpeed, VerbSetFlow, ArgFlow)
	}
}

func percentScript(command string, args map[string]string, key string) (string, error) {
	text, ok, err := numberArg(args, key)
	if err != nil {
		return "", err
	}

	if !ok {
		return "", malformed("", "the "+key+" argument is required")
	}

	return command + " S" + text, nil
}

func speedScript(args map[string]string) (string, error) { return percentScript("M220", args, "speed") }

func flowScript(args map[string]string) (string, error) { return percentScript("M221", args, "flow") }
