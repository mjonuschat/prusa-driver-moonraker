package moonraker

import (
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// A person answers the runout dialog with a button. Klipper has no dialogs of
// its own, so each button is one of the print verbs: Pause pauses the print,
// Resume resumes it and Stop cancels it.
const (
	VerbDialogAction = connectorapi.Capability("core.command.dialog_action")
	ArgDialogID      = connectorapi.Capability("core.command.dialog_action#dialog_id")
	ArgDialogButton  = connectorapi.Capability("core.command.dialog_action#button")

	dialogButtonsPath = "/dialog/buttons"

	buttonPause  = "Pause"
	buttonResume = "Resume"
	buttonStop   = "Stop"

	// maxDialogID is the largest dialog id Prusa's own firmware declares.
	maxDialogID = 4294967295

	// The lists written at /dialog/buttons, in display order. A running print is
	// offered Pause and a paused one Resume, because Klipper has nothing to
	// pause or resume otherwise. Stop cancels the print in both.
	buttonsPaused  = `["` + buttonResume + `","` + buttonStop + `"]`
	buttonsRunning = `["` + buttonPause + `","` + buttonStop + `"]`
)

// buttonsFor is the button list for a print in the given Klipper state.
func buttonsFor(printState string) string {
	if printState == "paused" {
		return buttonsPaused
	}

	return buttonsRunning
}

func dialogBaseline() connectorapi.Set {
	return connectorapi.Set{
		VerbDialogAction: {Flag: true},
		ArgDialogID: {
			Flag: true, Kind: connectorapi.KindInteger, Required: true,
			Min: 0, HasMin: true, Limit: maxDialogID, HasLimit: true,
		},
		ArgDialogButton: {Flag: true, Kind: connectorapi.KindString, Required: true},
	}
}

// carryDialog offers the verb when the machine has a filament sensor, because
// the runout dialog is the only dialog this driver raises.
func carryDialog(into, from connectorapi.Set, hasSensor bool) {
	if hasSensor {
		carry(into, from, VerbDialogAction, ArgDialogID, ArgDialogButton)
	}
}

// dialogCommand is the call a dialog answer becomes, and the print verb it
// stands for, whose state rule it follows.
func (s *Session) dialogCommand(cmd connectorapi.Command) (method string, verb connectorapi.Capability, err error) {
	id, parseErr := strconv.Atoi(cmd.Args["dialog_id"])
	if parseErr != nil || id != runoutDialogID || !s.dialogOpen.Load() {
		return "", "", fault.PrinterVerbNotAllowedInState.
			New("the printer has no open dialog with that id").
			With(fault.StringParam("verb", cmd.Verb.String()))
	}

	switch cmd.Args["button"] {
	case buttonPause:
		return methodPrintPause, VerbPausePrint, nil
	case buttonResume:
		return methodPrintResume, VerbResumePrint, nil
	case buttonStop:
		return methodPrintCancel, VerbStopPrint, nil
	default:
		return "", "", connectorapi.ArgumentMalformed.
			New("the dialog has no such button, and its buttons are Pause, Resume and Stop").
			With(fault.StringParam("verb", cmd.Verb.String()), fault.StringParam("argument", "button"))
	}
}
