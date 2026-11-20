package moonraker

import "github.com/prusa3d/prusa-connect/sdk/connectorapi"

// The chamber fan is a fan_generic the machine already has, set by a percentage
// from 0 to 100. Klipper has no automatic mode to hand it back to, so unlike
// Prusa's own descriptor there is no -1.
const (
	VerbSetChamberFan = connectorapi.Capability("core.command.set_chamber_fan_target")
	ArgChamberFan     = connectorapi.Capability("core.command.set_chamber_fan_target#chamber.fan_pwm_target")

	chamberFanArgName = "chamber.fan_pwm_target"
	chamberFanPath    = "/chamber/fan_pwm_target"
)

// chamberFanNames are the names a chamber fan is looked for under, best first.
// Only a fan_generic can be set, so a heater_fan or a controller_fan of the same
// name is not one.
//
//nolint:gochecknoglobals // a fixed list, read-only after init.
var chamberFanNames = []string{
	"chamber_fan", "chamber_fans", "enclosure_fan", "exhaust_fan", "filter_fan", "nevermore",
}

func defaultChamberFan(listed []string) string {
	for _, name := range chamberFanNames {
		if object := named(listed, "fan_generic "+name); object != "" {
			return object
		}
	}

	return ""
}

func chamberFanBaseline() connectorapi.Set {
	return connectorapi.Set{
		VerbSetChamberFan: {Flag: true},
		ArgChamberFan: {
			Flag: true, Kind: connectorapi.KindInteger, Required: true,
			Min: 0, HasMin: true, Limit: percentMax, HasLimit: true,
			Unit: connectorapi.UnitPercent,
		},
	}
}

func carryChamberFan(into, from connectorapi.Set, hasFan bool) {
	if hasFan {
		carry(into, from, VerbSetChamberFan, ArgChamberFan)
	}
}

// chamberFanScript is the gcode that sets the fan object to the percentage given.
func chamberFanScript(object string, args map[string]string) (string, error) {
	if object == "" {
		return "", malformed("", "the machine has no chamber fan")
	}

	percent, err := percentageArg(args, chamberFanArgName, "the fan speed is a percentage from 0 to 100")
	if err != nil {
		return "", err
	}

	return "SET_FAN_SPEED FAN=" + heaterName(object) + " SPEED=" + roundFraction(percent/percentMax), nil
}
