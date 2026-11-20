package moonraker

import (
	"encoding/json"
	"slices"
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	dialogIDPath    = "/dialog/id"
	dialogTitlePath = "/dialog/title"
	dialogTextPath  = "/dialog/text"

	// runoutDialogID is the id the runout dialog is written under. The host
	// turns a /dialog/id into the awaiting_operator issue.
	runoutDialogID = 1
)

type runout struct {
	// detected is the last filament_detected heard, nil until one is.
	detected *bool
	raised   bool
	// disabled is true once the sensor reports enabled as false.
	disabled bool
}

// appendRunout raises the runout dialog while the sensor reports no filament
// during a print, and removes it when either stops being true. It is a level
// rule evaluated on every reading, so a fresh session that starts on a parked
// print raises the dialog again. No button list is written, because the driver
// offers no dialog verb.
func (g *gauges) appendRunout(
	out []connectorapi.StatePatch, fields map[string]json.RawMessage, printState string,
) []connectorapi.StatePatch {
	if g.roles.filament == "" {
		return out
	}

	var detected bool
	if json.Unmarshal(fields["filament_detected"], &detected) == nil {
		g.runout.detected = &detected
	}

	var enabled bool
	if json.Unmarshal(fields["enabled"], &enabled) == nil {
		g.runout.disabled = !enabled
	}

	wanted := !g.runout.disabled && g.runout.detected != nil && !*g.runout.detected &&
		slices.Contains(klipperPrintingStates, printState)

	switch {
	case wanted && !g.runout.raised:
		g.runout.raised = true
		out = g.put(out, dialogIDPath, json.RawMessage(strconv.Itoa(runoutDialogID)))
		out = g.put(out, dialogTitlePath, json.RawMessage(`"Filament runout"`))

		return g.put(out, dialogTextPath, json.RawMessage(`"The filament sensor reports no filament."`))
	case !wanted && g.runout.raised:
		g.runout.raised = false
		out = g.drop(out, dialogIDPath)
		out = g.drop(out, dialogTitlePath)

		return g.drop(out, dialogTextPath)
	}

	return out
}
