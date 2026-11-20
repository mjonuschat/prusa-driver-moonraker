package moonraker

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	VerbMove            = connectorapi.Capability("core.command.move")
	ArgMoveFeedrate     = connectorapi.Capability("move#feedrate")
	ArgMoveX            = connectorapi.Capability("move#x")
	ArgMoveY            = connectorapi.Capability("move#y")
	VerbMoveZ           = connectorapi.Capability("core.command.move_z")
	ArgMoveZFeedrate    = connectorapi.Capability("move_z#feedrate")
	ArgMoveZDistance    = connectorapi.Capability("move_z#distance")
	VerbHome            = connectorapi.Capability("core.command.home")
	ArgHomeAxis         = connectorapi.Capability("home#axis")
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
