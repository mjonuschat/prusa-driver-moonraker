package moonraker

// The states this dialect reports at /state, the moves between them, and the
// verbs each one accepts. The words are the values of stateWords in
// telemetry.go. A word Klipper adds that the table does not carry still reaches
// the tree in upper case, and a client then finds no entry for it here.
//
// Starting a print, a script and the two resets are guarded as well: a print
// does not start over one that is running, and a machine in ERROR takes a
// reset and an emergency stop and nothing else that moves it.
//
// The guards follow Klipper's own pause_resume section: PAUSE does nothing to a
// print that is already paused, and RESUME does nothing to one that is not.
// Neither is an error on the wire, so a guard is the only place a client can
// learn it. A heater target and a status read are accepted in every state.

import (
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The state words, as stateWords writes them.
const (
	stateIdle     connectorapi.StateName = "IDLE"
	statePrinting connectorapi.StateName = "PRINTING"
	statePaused   connectorapi.StateName = "PAUSED"
	stateFinished connectorapi.StateName = "FINISHED"
	stateStopped  connectorapi.StateName = "STOPPED"
	stateError    connectorapi.StateName = "ERROR"
	// stateBusy is Klipper starting, after a restart or while Moonraker waits
	// for it. It is not a fault, and the session stays connected through it.
	stateBusy connectorapi.StateName = "BUSY"
)

// states is this dialect's declared state set.
func states() connectorapi.StateSet {
	idle := []connectorapi.Capability{VerbSendStateInfo, VerbNozzleTarget, VerbBedTarget, VerbChamberTarget}
	files := []connectorapi.Capability{
		connectorapi.VerbSendInfo, connectorapi.VerbSendFileInfo,
		connectorapi.VerbCreateFolder, connectorapi.VerbDeleteFile, connectorapi.VerbDeleteFolder,
		connectorapi.VerbFileUpload,
	}
	motion := []connectorapi.Capability{VerbMove, VerbMoveZ, VerbMoveE, VerbHome, VerbDisableSteppers}
	// Levelling is held back while a print is on the bed, paused or not.
	levelling := []connectorapi.Capability{VerbMeshBedLeveling}
	// A speed or flow override changes how the print runs, so it is accepted
	// beside a print, and refused in ERROR.
	tuning := []connectorapi.Capability{VerbSetSpeed, VerbSetFlow, VerbSetChamberLED, VerbSetChamberFan}
	// An object is cancelled or brought back while its print is on the bed.
	objects := []connectorapi.Capability{VerbCancelObject, VerbUncancelObject}
	// Loading and unloading filament is held back while a print is running. It
	// is allowed paused, which is where a runout is dealt with.
	filament := []connectorapi.Capability{VerbLoadFilament, VerbUnloadFilament}
	// The runout dialog is answered while the print is on the bed.
	dialog := []connectorapi.Capability{VerbDialogAction}
	// The result of a print is cleared once it has ended, and not before: the
	// reset stops a print that is still running.
	ended := []connectorapi.Capability{VerbSetIdle}
	always := []connectorapi.Capability{VerbResetPrinter, VerbReset, VerbForceGCode}
	scripted := append([]connectorapi.Capability{VerbSendGCode}, always...)
	startable := append([]connectorapi.Capability{VerbStartPrint}, scripted...)

	accepts := func(lists ...[]connectorapi.Capability) []connectorapi.Capability {
		return slices.Concat(lists...)
	}

	return connectorapi.StateSet{
		States: []connectorapi.DeviceState{
			{Name: stateIdle, DisplayName: "Idle", Accepts: accepts(startable, idle, files, motion, levelling, tuning, filament)},
			{Name: statePrinting, DisplayName: "Printing", Accepts: accepts(
				[]connectorapi.Capability{VerbPausePrint, VerbStopPrint}, scripted, idle, files, tuning, objects, dialog)},
			{Name: statePaused, DisplayName: "Paused", Accepts: accepts(
				[]connectorapi.Capability{VerbResumePrint, VerbStopPrint}, scripted, idle, files, motion, tuning, objects, filament, dialog)},
			{Name: stateFinished, DisplayName: "Finished", Accepts: accepts(startable, ended, idle, files, motion, levelling, tuning, filament)},
			{Name: stateStopped, DisplayName: "Stopped", Accepts: accepts(startable, ended, idle, files, motion, levelling, tuning, filament)},
			{Name: stateError, DisplayName: "Error", Accepts: accepts(always, idle, files)},
			// Klipper refuses scripts and motion while it starts, so BUSY takes
			// what ERROR takes: a reset, an emergency stop, reads and files.
			{Name: stateBusy, DisplayName: "Busy", Accepts: accepts(always, idle, files)},
		},
		Transitions: []connectorapi.Transition{
			{From: stateIdle, To: statePrinting, Verb: VerbStartPrint},
			{From: stateFinished, To: statePrinting, Verb: VerbStartPrint},
			{From: stateStopped, To: statePrinting, Verb: VerbStartPrint},
			{From: stateError, To: statePrinting},
			{From: statePrinting, To: statePaused, Verb: VerbPausePrint},
			{From: statePaused, To: statePrinting, Verb: VerbResumePrint},
			{From: statePrinting, To: stateStopped, Verb: VerbStopPrint},
			{From: statePaused, To: stateStopped, Verb: VerbStopPrint},
			{From: statePrinting, To: stateFinished},
			{From: statePrinting, To: stateError},
			{From: stateIdle, To: stateError, Verb: VerbForceGCode},
			{From: statePrinting, To: stateError, Verb: VerbForceGCode},
			{From: statePaused, To: stateError, Verb: VerbForceGCode},
			{From: stateFinished, To: stateError, Verb: VerbForceGCode},
			{From: stateStopped, To: stateError, Verb: VerbForceGCode},
			{From: stateFinished, To: stateIdle},
			{From: stateStopped, To: stateIdle},
			{From: stateFinished, To: stateIdle, Verb: VerbSetIdle},
			{From: stateStopped, To: stateIdle, Verb: VerbSetIdle},
			{From: stateError, To: stateIdle},
			{From: stateError, To: stateIdle, Verb: VerbResetPrinter},
			{From: stateError, To: stateIdle, Verb: VerbReset},
			{From: stateError, To: stateFinished},
			{From: stateError, To: stateStopped},
			{From: stateError, To: statePaused},
			// A restart, from this driver or from anywhere else, takes the
			// machine through BUSY while Klipper starts again. It comes out
			// ready, or in ERROR when it could not start.
			{From: stateIdle, To: stateBusy},
			{From: statePrinting, To: stateBusy},
			{From: statePaused, To: stateBusy},
			{From: stateFinished, To: stateBusy},
			{From: stateStopped, To: stateBusy},
			{From: stateError, To: stateBusy},
			{From: stateIdle, To: stateBusy, Verb: VerbResetPrinter},
			{From: stateIdle, To: stateBusy, Verb: VerbReset},
			{From: statePrinting, To: stateBusy, Verb: VerbResetPrinter},
			{From: statePrinting, To: stateBusy, Verb: VerbReset},
			{From: statePaused, To: stateBusy, Verb: VerbResetPrinter},
			{From: statePaused, To: stateBusy, Verb: VerbReset},
			{From: stateFinished, To: stateBusy, Verb: VerbResetPrinter},
			{From: stateFinished, To: stateBusy, Verb: VerbReset},
			{From: stateStopped, To: stateBusy, Verb: VerbResetPrinter},
			{From: stateStopped, To: stateBusy, Verb: VerbReset},
			{From: stateError, To: stateBusy, Verb: VerbResetPrinter},
			{From: stateError, To: stateBusy, Verb: VerbReset},
			{From: stateBusy, To: stateIdle},
			{From: stateBusy, To: stateFinished},
			{From: stateBusy, To: stateStopped},
			{From: stateBusy, To: stateError},
		},
	}
}
