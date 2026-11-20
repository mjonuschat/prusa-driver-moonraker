package moonraker

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The chamber light is one Klipper object: an output pin or an LED strip. It
// is set by a percentage from 0 to 100.
const (
	VerbSetChamberLED = connectorapi.Capability("core.command.set_chamber_led_intensity")
	ArgChamberLED     = connectorapi.Capability("core.command.set_chamber_led_intensity#chamber.led_intensity")

	chamberLEDArgName = "chamber.led_intensity"
	lightPath         = "/chamber/led_intensity"

	percentMax = 100.0
)

// lightKinds are the object types a chamber light can be.
//
//nolint:gochecknoglobals // a fixed list, read-only after init.
var lightKinds = []string{"output_pin", "led", "neopixel", "dotstar"}

// lightNames are the names a chamber light is looked for under, best first. A
// case light is the last resort, so a machine with both uses its chamber light.
//
//nolint:gochecknoglobals // a fixed list, read-only after init.
var lightNames = []string{"chamber_light", "chamber_led", "chamber_lights", "caselight", "case_light"}

// defaultLight is the first chamber light the machine lists.
func defaultLight(listed []string) string {
	for _, name := range lightNames {
		for _, kind := range lightKinds {
			if object := named(listed, kind+" "+name); object != "" {
				return object
			}
		}
	}

	return ""
}

func chamberLightBaseline() connectorapi.Set {
	return connectorapi.Set{
		VerbSetChamberLED: {Flag: true},
		ArgChamberLED: {
			Flag: true, Kind: connectorapi.KindInteger, Required: true,
			Min: 0, HasMin: true, Limit: percentMax, HasLimit: true,
			Unit: connectorapi.UnitPercent,
		},
	}
}

func carryChamberLight(into, from connectorapi.Set, hasLight bool) {
	if hasLight {
		carry(into, from, VerbSetChamberLED, ArgChamberLED)
	}
}

// lightControl is how the chamber light is set, as its configuration says.
type lightControl struct {
	// pin is true for an output pin and false for an LED strip.
	pin  bool
	name string
	// pwm and scale are an output pin's: a pin that is not PWM takes only 0 or
	// the scale, and a value is written on a scale of 0 to scale.
	pwm   bool
	scale float64
	// white is whether an LED strip has a white channel.
	white bool
}

type pinSection struct {
	PWM   *bool    `json:"pwm"`
	Scale *float64 `json:"scale"`
}

type ledSection struct {
	ColorOrder json.RawMessage `json:"color_order"`
}

// lightControlOf reads how the light object is set from the configuration. A
// configuration that cannot be read leaves a pin as a plain PWM pin, so Klipper
// has the last word on what it accepts.
func lightControlOf(object string, dumped configDump) lightControl {
	kind, name, _ := strings.Cut(object, " ")
	control := lightControl{pin: strings.EqualFold(kind, "output_pin"), name: name}

	var raw json.RawMessage

	for section, body := range dumped.Settings {
		if strings.EqualFold(section, object) {
			raw = body

			break
		}
	}

	if control.pin {
		control.pwm, control.scale = true, 1

		var section pinSection
		if raw != nil && json.Unmarshal(raw, &section) == nil {
			control.pwm = section.PWM != nil && *section.PWM

			if section.Scale != nil && finitePositive(*section.Scale) {
				control.scale = *section.Scale
			}
		}

		return control
	}

	var section ledSection
	if raw != nil && json.Unmarshal(raw, &section) == nil {
		control.white = strings.Contains(strings.ToUpper(string(section.ColorOrder)), "W")
	}

	return control
}

// roundFraction writes a number in plain decimal, rounded so that 0.3 times
// 100 is 30 and not 30.000000000000004.
func roundFraction(value float64) string {
	return strconv.FormatFloat(math.Round(value*1e6)/1e6, 'f', -1, 64)
}

// percentageArg reads a required percentage and preserves the command's error message.
func percentageArg(args map[string]string, key, invalidReason string) (float64, error) {
	text, ok, err := plainValue(args, key)
	if err != nil {
		return 0, err
	}

	if !ok {
		return 0, malformed("", "the "+key+" argument is required")
	}

	percent, parseErr := strconv.ParseFloat(text, 64)
	if parseErr != nil || math.IsNaN(percent) || percent < 0 || percent > percentMax {
		return 0, malformed(text, invalidReason)
	}

	return percent, nil
}

// lightScript is the gcode that sets the light to the percentage given.
func lightScript(control lightControl, args map[string]string) (string, error) {
	percent, err := percentageArg(args, chamberLEDArgName, "the light intensity is a percentage from 0 to 100")
	if err != nil {
		return "", err
	}

	fraction := percent / percentMax

	if control.pin {
		value := fraction * control.scale
		if !control.pwm {
			value = 0
			if percent > 0 {
				value = control.scale
			}
		}

		return fmt.Sprintf("SET_PIN PIN=%s VALUE=%s", control.name, roundFraction(value)), nil
	}

	level := roundFraction(fraction)
	script := fmt.Sprintf("SET_LED LED=%s RED=%s GREEN=%s BLUE=%s", control.name, level, level, level)

	if control.white {
		script += " WHITE=" + level
	}

	return script, nil
}

// lightPercent is the intensity an LED strip's colour data stands for: the
// brightest channel of its first LED, as a fraction. ok is false for data that
// names no LED.
func lightPercent(raw json.RawMessage) (fraction float64, ok bool) {
	var colours [][]float64
	if json.Unmarshal(raw, &colours) != nil || len(colours) == 0 {
		return 0, false
	}

	for _, channel := range colours[0] {
		fraction = max(fraction, channel)
	}

	return fraction, true
}
