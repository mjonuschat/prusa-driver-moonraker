package moonraker

import (
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	// maxRawGCode is the payload length in bytes, declared in the descriptor.
	maxRawGCode = 256

	// The printable ASCII range. Every other byte is refused, which stops a
	// second command being smuggled behind a newline.
	firstPrintable = 0x20
	lastPrintable  = 0x7E

	// letterAndDigit is the shortest word this parser accepts, such as "G0".
	letterAndDigit = 2

	// commandLetters are the letters a payload may start with.
	commandLetters = "GMT"

	// emergencyWord is the one word the force verb carries.
	emergencyWord = "M112"
)

// refusedWords are the gcode words never sent, keyed by letter and whole-number
// part so that a decimal spelling cannot walk past the refusal. It is a
// denylist, so a deployment that wants an allowlist narrows the argument's
// Supported set in a profile.
//
//nolint:gochecknoglobals // a closed set read on every command, never written.
var refusedWords = map[string]string{
	"M28": "a file is written through the file capabilities, which check the path",
	"M29": "a file is written through the file capabilities, which check the path",
	"M30": "a file is deleted through the file capabilities, which check the path",
}

// gcodeLine checks a raw gcode payload and returns it trimmed.
//
// The order matters. Printable ASCII first, because a payload with a newline is
// two commands and the word check would see only the first. Then the parse, so
// that a refused word has no spelling that reaches the wire. Then the word.
func gcodeLine(payload string) (string, error) {
	if err := refuseUnprintable(payload); err != nil {
		return "", err
	}

	trimmed := strings.Trim(payload, " ")
	if trimmed == "" {
		return "", malformed(payload, "the payload is blank")
	}

	word, err := commandWord(trimmed)
	if err != nil {
		return "", err
	}

	if reason, refused := refusedWords[word]; refused {
		return "", notAllowed(trimmed, word, reason)
	}

	return trimmed, nil
}

// emergencyStop accepts only M112, which is sent as printer.emergency_stop and
// not as a script.
func emergencyStop(payload string) error {
	trimmed := strings.Trim(payload, " ")
	if trimmed == emergencyWord {
		return nil
	}

	return notAllowed(trimmed, trimmed, "a forced command is an emergency stop and nothing else")
}

// refuseUnprintable rejects any byte outside printable ASCII. It loops over
// bytes so that a multi-byte rune or invalid UTF-8 is refused the same way.
func refuseUnprintable(payload string) error {
	for i := range len(payload) {
		if payload[i] < firstPrintable || payload[i] > lastPrintable {
			return malformed(payload,
				"the payload carries a byte outside printable ASCII, and a payload is one line")
		}
	}

	return nil
}

// commandWord returns the uppercased letter and whole number a payload starts
// with. The digit run is greedy, so M2830 is not M28, and the decimal part is
// dropped, so M28.1 is M28. Nothing is required after the number, because
// Klipper reads M104S215 as M104.
func commandWord(payload string) (string, error) {
	if len(payload) < letterAndDigit {
		return "", malformed(payload, "the payload is shorter than one gcode word")
	}

	letter := upper(payload[0])
	if !strings.ContainsRune(commandLetters, rune(letter)) {
		return "", malformed(payload, "a command starts with G, M or T followed by its number")
	}

	digits := countDigits(payload[1:])
	if digits == 0 {
		return "", malformed(payload, "the letter is not followed by a number")
	}

	return string(letter) + payload[1:1+digits], nil
}

// countDigits returns how many ASCII digits s starts with.
func countDigits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}

	return n
}

// upper folds one ASCII letter without allocating.
func upper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - ('a' - 'A')
	}

	return b
}

func malformed(payload, reason string) error {
	return connectorapi.GCodeMalformed.New(reason).
		With(fault.StringParam("gcode", payload))
}

func notAllowed(payload, word, reason string) error {
	return connectorapi.GCodeNotAllowed.New("this server never sends that gcode word").
		With(fault.StringParam("gcode", payload),
			fault.StringParam("word", word),
			fault.StringParam("reason", reason))
}
