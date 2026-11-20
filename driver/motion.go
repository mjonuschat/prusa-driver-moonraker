package moonraker

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	VerbMove            = connectorapi.Capability("core.command.move")
	ArgMoveFeedrate     = connectorapi.Capability("core.command.move#feedrate")
	ArgMoveX            = connectorapi.Capability("core.command.move#x")
	ArgMoveY            = connectorapi.Capability("core.command.move#y")
	VerbMoveZ           = connectorapi.Capability("core.command.move_z")
	ArgMoveZFeedrate    = connectorapi.Capability("core.command.move_z#feedrate")
	ArgMoveZDistance    = connectorapi.Capability("core.command.move_z#distance")
	VerbHome            = connectorapi.Capability("core.command.home")
	ArgHomeAxis         = connectorapi.Capability("core.command.home#axis")
	VerbDisableSteppers = connectorapi.Capability("core.command.disable_steppers")
)

const (
	// jogState names the saved gcode state of a jog. It is never PAUSE_STATE,
	// which a pause owns.
	jogState = "CONNECT_JOG"

	disableSteppersScript = "M84"
	restoreJogScript      = "RESTORE_GCODE_STATE NAME=" + jogState

	saveJogScript = "SAVE_GCODE_STATE NAME=" + jogState
)

func jogScript(move string) string {
	return strings.Join([]string{saveJogScript, "G91", move, restoreJogScript}, "\n")
}

// plainValue returns an argument value after refusing control characters and
// ';', which would start a comment or a second command.
func plainValue(args map[string]string, key string) (string, bool, error) {
	value, ok := args[key]
	if !ok {
		return "", false, nil
	}

	for _, r := range value {
		if unicode.IsControl(r) || r == ';' {
			return "", false, malformed(value, "the "+key+" argument holds a control character or ';'")
		}
	}

	return value, true, nil
}

func numberArg(args map[string]string, key string) (string, bool, error) {
	value, ok, err := plainValue(args, key)
	if err != nil || !ok {
		return "", false, err
	}

	text, err := formatMillimetres(value)
	if err != nil {
		return "", false, err
	}

	return text, true, nil
}

func feedrateArg(args map[string]string) (string, error) {
	value, ok, err := plainValue(args, "feedrate")
	if err != nil {
		return "", err
	}

	if !ok {
		return "", malformed("", "the feedrate argument is required")
	}

	rate, err := strconv.ParseUint(value, 10, 32)
	if err != nil || rate < 1 {
		return "", malformed(value, "the feedrate is not a whole number of at least 1")
	}

	return strconv.FormatUint(rate, 10), nil
}

// formatMillimetres writes a finite number in plain decimal, never in
// exponent notation, with negative zero as 0.
func formatMillimetres(text string) (string, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", malformed(text, "the value is not a finite number")
	}

	if value == 0 {
		return "0", nil
	}

	return strconv.FormatFloat(value, 'f', -1, 64), nil
}

func moveScript(args map[string]string) (string, error) {
	feedrate, err := feedrateArg(args)
	if err != nil {
		return "", err
	}

	words := []string{"G1"}

	for _, axis := range []string{"x", "y"} {
		text, ok, err := numberArg(args, axis)
		if err != nil {
			return "", err
		}

		if ok {
			words = append(words, strings.ToUpper(axis)+text)
		}
	}

	if len(words) == 1 {
		return "", malformed("", "a move needs an x or a y")
	}

	return jogScript(strings.Join(append(words, "F"+feedrate), " ")), nil
}

func moveZScript(args map[string]string) (string, error) {
	feedrate, err := feedrateArg(args)
	if err != nil {
		return "", err
	}

	distance, ok, err := numberArg(args, "distance")
	if err != nil {
		return "", err
	}

	if !ok {
		return "", malformed("", "the distance argument is required")
	}

	return jogScript("G1 Z" + distance + " F" + feedrate), nil
}

func homeScript(args map[string]string) (string, error) {
	value, _, err := plainValue(args, "axis")
	if err != nil {
		return "", err
	}

	var seen [3]bool

	for _, r := range strings.ToUpper(value) {
		switch r {
		case ' ', ',':
		case 'X':
			seen[0] = true
		case 'Y':
			seen[1] = true
		case 'Z':
			seen[2] = true
		default:
			return "", malformed(value, "an axis is x, y or z")
		}
	}

	words := []string{"G28"}

	for i, letter := range []string{"X", "Y", "Z"} {
		if seen[i] {
			words = append(words, letter)
		}
	}

	return strings.Join(words, " "), nil
}

// The outer ceilings the baseline declares. They never narrow a machine.
// The probe narrows them to the machine's own figures.
const (
	moveFeedrateCeiling  = 60000
	moveZFeedrateCeiling = 6000
	stepCeiling          = 5000

	// The fallbacks the probe uses only when the machine's own figure cannot
	// be read. They live in the probe because the handshake intersects the
	// baseline with it.
	moveFeedrateFallback  = 6000
	moveZFeedrateFallback = 600
	stepFallback          = 1000
)

func feedrateArgument(ceiling float64) connectorapi.Value {
	return connectorapi.Value{
		Flag: true, Kind: connectorapi.KindInteger, Required: true,
		Min: 1, HasMin: true, Limit: ceiling, HasLimit: true,
		Unit: connectorapi.UnitMillimeterPerMinute,
	}
}

func stepArgument(required bool) connectorapi.Value {
	return connectorapi.Value{
		Flag: true, Kind: connectorapi.KindNumber, Required: required,
		Min: -stepCeiling, HasMin: true, Limit: stepCeiling, HasLimit: true,
		Unit: connectorapi.UnitMillimeter,
	}
}

func motionBaseline() connectorapi.Set {
	return connectorapi.Set{
		VerbMove:            {Flag: true},
		ArgMoveFeedrate:     feedrateArgument(moveFeedrateCeiling),
		ArgMoveX:            stepArgument(false),
		ArgMoveY:            stepArgument(false),
		VerbMoveZ:           {Flag: true},
		ArgMoveZFeedrate:    feedrateArgument(moveZFeedrateCeiling),
		ArgMoveZDistance:    stepArgument(true),
		VerbHome:            {Flag: true},
		ArgHomeAxis:         {Flag: true, Kind: connectorapi.KindString},
		VerbDisableSteppers: {Flag: true},
	}
}

// motionLimits are the machine's own figures, with zero meaning unknown.
type motionLimits struct {
	maxVelocity, maxZVelocity float64
	maxExtrudeVelocity        float64
	maxExtrudeDistance        float64
	travel                    [3]float64
}

type printerSection struct {
	MaxVelocity  *float64 `json:"max_velocity"`
	MaxZVelocity *float64 `json:"max_z_velocity"`
}

func finitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

func motionLimitsOf(dumped configDump) motionLimits {
	var out motionLimits

	var printer printerSection
	if json.Unmarshal(dumped.Settings["printer"], &printer) == nil {
		if printer.MaxVelocity != nil && finitePositive(*printer.MaxVelocity) {
			out.maxVelocity = *printer.MaxVelocity
		}

		if printer.MaxZVelocity != nil && finitePositive(*printer.MaxZVelocity) {
			out.maxZVelocity = *printer.MaxZVelocity
		}
	}

	readExtruderLimits(dumped, &out)

	for i, axis := range axisSections {
		var bounds stepperBounds
		if json.Unmarshal(dumped.Settings[axis.section], &bounds) != nil || bounds.PositionMax == nil {
			continue
		}

		low := 0.0
		if bounds.PositionMin != nil {
			low = *bounds.PositionMin
		}

		if span := *bounds.PositionMax - low; finitePositive(span) {
			out.travel[i] = span
		}
	}

	return out
}

func carryMotion(into, from connectorapi.Set, limits motionLimits) {
	carry(into, from, VerbMove, VerbMoveZ, VerbHome, VerbDisableSteppers, ArgHomeAxis)

	rate := func(key connectorapi.Capability, velocity, fallback float64) {
		declared, present := from[key]
		if !present {
			return
		}

		figure := fallback
		if velocity > 0 {
			figure = velocity * 60
		}

		narrowLimit(&declared, figure)
		into[key] = declared
	}

	rate(ArgMoveFeedrate, limits.maxVelocity, moveFeedrateFallback)
	rate(ArgMoveZFeedrate, limits.maxZVelocity, moveZFeedrateFallback)

	step := func(key connectorapi.Capability, span float64) {
		declared, present := from[key]
		if !present {
			return
		}

		if span <= 0 {
			span = stepFallback
		}

		narrowLimit(&declared, span)

		if !declared.HasMin || declared.Min < -span {
			declared.Min = -span
			declared.HasMin = true
		}

		into[key] = declared
	}

	step(ArgMoveX, limits.travel[0])
	step(ArgMoveY, limits.travel[1])
	step(ArgMoveZDistance, limits.travel[2])
}

func narrowLimit(value *connectorapi.Value, figure float64) {
	if !value.HasLimit || figure < value.Limit {
		value.Limit = figure
		value.HasLimit = true
	}
}

func isJog(verb connectorapi.Capability) bool {
	return verb == VerbMove || verb == VerbMoveZ || verb == VerbMoveE
}

func isMotion(verb connectorapi.Capability) bool {
	return isJog(verb) || verb == VerbHome || verb == VerbDisableSteppers || verb == VerbMeshBedLeveling ||
		verb == VerbLoadFilament || verb == VerbUnloadFilament
}

// takesMotionLock is true for the verbs that must not interleave with a jog:
// the motion verbs, and the ones that start, resume or end a print or send
// gcode. force_gcode is left out so an emergency stop never waits.
func takesMotionLock(verb connectorapi.Capability) bool {
	switch verb {
	case VerbPausePrint, VerbResumePrint, VerbStopPrint, VerbStartPrint, VerbSendGCode, VerbSetSpeed, VerbSetFlow:
		return true
	default:
		return isMotion(verb)
	}
}
