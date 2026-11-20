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
	// buttons is the list written at /dialog/buttons while the dialog is raised.
	buttons string
	// disabled is true once the sensor reports enabled as false.
	disabled bool
}

// appendRunout raises the runout dialog while the sensor reports no filament
// during a print, and removes it when either stops being true. It is a level
// rule evaluated on every reading, so a fresh session that starts on a parked
// print raises the dialog again. The buttons are Resume, only while the print
// is paused, and Stop, which the dialog action verb carries out.
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

	buttons := buttonsFor(printState)

	switch {
	case wanted && g.runout.raised && buttons != g.runout.buttons:
		g.runout.buttons = buttons

		return g.put(out, dialogButtonsPath, json.RawMessage(buttons))
	case wanted && !g.runout.raised:
		g.runout.raised = true
		g.runout.buttons = buttons
		out = g.put(out, dialogIDPath, json.RawMessage(strconv.Itoa(runoutDialogID)))
		out = g.put(out, dialogTitlePath, json.RawMessage(`"Filament runout"`))
		out = g.put(out, dialogTextPath, json.RawMessage(`"The filament sensor reports no filament."`))

		return g.put(out, dialogButtonsPath, json.RawMessage(buttons))
	case !wanted && g.runout.raised:
		g.runout.raised = false
		out = g.drop(out, dialogIDPath)
		out = g.drop(out, dialogTitlePath)
		out = g.drop(out, dialogTextPath)

		return g.drop(out, dialogButtonsPath)
	}

	return out
}
