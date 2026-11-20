package moonraker

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The capabilities this dialect can carry out. Every key is one the published
// vocabulary already names, so a client that drives a Prusa drives a Klipper
// machine with the same words.
const (
	// VerbSendStateInfo reads the machine's state and moves nothing, which is
	// what makes it the verb a conformance run may send.
	VerbSendStateInfo = connectorapi.Capability("core.command.send_state_info")
	// VerbPausePrint pauses the running print.
	VerbPausePrint = connectorapi.Capability("core.command.pause_print")
	// VerbResumePrint resumes a paused print.
	VerbResumePrint = connectorapi.Capability("core.command.resume_print")
	// VerbStopPrint cancels the running print.
	VerbStopPrint = connectorapi.Capability("core.command.stop_print")
	// VerbNozzleTarget sets the hotend's target temperature.
	VerbNozzleTarget = connectorapi.Capability("core.command.set_nozzle_temperature")
	// ArgNozzleTarget is the hotend target, in whole degrees celsius.
	ArgNozzleTarget = connectorapi.Capability("core.command.set_nozzle_temperature#nozzle_temperature")
	// ArgNozzleTool is the head whose target is set, counted from 0 as the
	// head ids under /tools are: 0 is `extruder`, 1 is `extruder1`. Without
	// it, the head printing now.
	ArgNozzleTool = connectorapi.Capability("core.command.set_nozzle_temperature#tool")
	// VerbBedTarget sets the bed's target temperature.
	VerbBedTarget = connectorapi.Capability("core.command.set_bed_temperature")
	// ArgBedTarget is the bed target, in whole degrees celsius.
	ArgBedTarget = connectorapi.Capability("core.command.set_bed_temperature#bed_temperature")
	// VerbChamberTarget sets the chamber heater's target temperature.
	VerbChamberTarget = connectorapi.Capability("core.command.set_chamber_target_temp")
	// ArgChamberTarget is the chamber target, in whole degrees celsius.
	ArgChamberTarget = connectorapi.Capability("core.command.set_chamber_target_temp#chamber.target_temp")
	// VerbStartPrint starts printing a file the machine already holds.
	VerbStartPrint = connectorapi.Capability("core.command.start_print")
	// ArgStartPath is the file to print, as the storage-prefixed path List
	// returns, such as gcodes/models/part.gcode.
	ArgStartPath = connectorapi.Capability("core.command.start_print#path")
	// VerbResetPrinter restarts the Klipper host software.
	VerbResetPrinter = connectorapi.Capability("core.command.reset_printer")
	// VerbReset restarts the host software and the machine's firmware.
	VerbReset = connectorapi.Capability("core.command.reset")
	// VerbSendGCode runs one line of gcode.
	VerbSendGCode = connectorapi.Capability("core.command.send_gcode")
	// ArgGCode is the line to run.
	ArgGCode = connectorapi.Capability("core.command.send_gcode#gcode")
	// VerbForceGCode is the out-of-band command. Here it is an emergency stop.
	VerbForceGCode = connectorapi.Capability("core.command.force_gcode")
	// ArgForceGCode is the forced word, which can only be M112.
	ArgForceGCode = connectorapi.Capability("core.command.force_gcode#gcode")
)

// The highest target this driver's code will ever pass on, in degrees celsius.
// They are the ceiling and not a machine's answer: a Klipper hotend is
// configured anywhere from 260 to 500, and the machine's own max_temp narrows
// these the moment it is read.
const (
	nozzleCeiling  = 500
	bedCeiling     = 250
	chamberCeiling = 250
)

// The Klipper heater names the two temperature verbs address. They are the
// names Klipper's own configuration sections carry, and a machine that renamed
// one reports no such object and is not offered the verb.
const (
	heaterExtruder = "extruder"
	heaterBed      = "heater_bed"
)

// The argument names the two temperature verbs carry, as the vocabulary spells
// them.
const (
	nozzleArgument  = "nozzle_temperature"
	toolArgument    = "tool"
	bedArgument     = "bed_temperature"
	chamberArgument = "chamber.target_temp"
)

// toolArgumentValue is the head argument of the nozzle verb, from 0 with no
// upper bound. The machine's own list of extruders bounds it in probe.
func toolArgumentValue() connectorapi.Value {
	return connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger, Min: 0, HasMin: true}
}

// temperatureArgument is one heater target's descriptor, bounded from zero to
// the ceiling given.
func temperatureArgument(ceiling float64) connectorapi.Value {
	return connectorapi.Value{
		Flag:     true,
		Kind:     connectorapi.KindInteger,
		Required: true,
		Min:      0,
		HasMin:   true,
		Limit:    ceiling,
		HasLimit: true,
		Unit:     connectorapi.UnitCelsius,
	}
}

// probe is what this machine says it can do, as a tier that narrows the two
// above it.
//
// It carries a key only when the machine reports the Klipper object that
// carries out the verb. A key it leaves out is subtracted, and a key it carries
// is copied from the resolved set so that the intersection changes nothing but
// the heater bounds this function sets on purpose.
func probe(
	available []string, heaters map[string]heaterConfig, extruders int, gcodes gcodesState, chamber chamberRole,
	limits motionLimits, resolved connectorapi.Set,
) connectorapi.Set {
	claimed := connectorapi.Set{}

	// Every machine answers a status query, so the one verb that reads and
	// moves nothing needs no object behind it.
	carry(claimed, resolved, VerbSendStateInfo)

	// PAUSE, RESUME and CANCEL_PRINT are registered by Klipper's pause_resume
	// section. A machine without it answers the three calls with an error, so
	// offering them would be offering something that cannot happen.
	if slices.Contains(available, objectPauseResume) {
		carry(claimed, resolved, VerbPausePrint, VerbResumePrint, VerbStopPrint)
	}

	// Klipper's virtual_sdcard section is what printer.print.start loads from.
	if slices.Contains(available, objectVirtualSDCard) {
		carry(claimed, resolved, VerbStartPrint, ArgStartPath)
	}

	// Listing needs the gcodes root. Changing it also needs the root to be
	// writable.
	if gcodes.present {
		carry(claimed, resolved, connectorapi.VerbSendInfo, connectorapi.VerbSendFileInfo)
	}

	if gcodes.present && gcodes.writable {
		carry(claimed, resolved,
			connectorapi.VerbCreateFolder, connectorapi.VerbDeleteFile, connectorapi.VerbDeleteFolder,
			connectorapi.VerbFileUpload)
	}

	carry(claimed, resolved, VerbResetPrinter, VerbReset)
	carry(claimed, resolved, VerbSendGCode, ArgGCode, VerbForceGCode, ArgForceGCode)

	if slices.Contains(available, objectExtruder) {
		carryHeater(claimed, resolved, VerbNozzleTarget, ArgNozzleTarget, heaters[heaterExtruder])
		carryTool(claimed, resolved, extruders)
	}

	if slices.Contains(available, objectHeaterBed) {
		carryHeater(claimed, resolved, VerbBedTarget, ArgBedTarget, heaters[heaterBed])
	}

	// Only a chamber that is a heater has a target to set. A sensor, or an
	// override naming a non-heater, is offered nothing.
	if chamber.heater {
		carryHeater(claimed, resolved, VerbChamberTarget, ArgChamberTarget, heaters[strings.ToLower(chamber.object)])
	}

	if slices.Contains(available, objectToolhead) {
		carryMotion(claimed, resolved, limits)
	}

	return claimed
}

// carry copies keys from the resolved set into the machine's tier unchanged, so
// that intersecting the two subtracts what the machine did not name and changes
// nothing else.
func carry(into, from connectorapi.Set, keys ...connectorapi.Capability) {
	for _, key := range keys {
		if value, declared := from[key]; declared {
			into[key] = value
		}
	}
}

// carryHeater copies one temperature verb and narrows its argument to the range
// the machine's own configuration declares.
func carryHeater(
	into, from connectorapi.Set,
	verb, argument connectorapi.Capability,
	configured heaterConfig,
) {
	carry(into, from, verb)

	declared, present := from[argument]
	if !present {
		return
	}

	if configured.MaxTemp != nil {
		declared.Limit = *configured.MaxTemp
		declared.HasLimit = true
	}

	if configured.MinTemp != nil {
		declared.Min = *configured.MinTemp
		declared.HasMin = true
	}

	into[argument] = declared
}

// carryTool copies the head argument of the nozzle verb and bounds it to the
// last head the machine lists.
func carryTool(into, from connectorapi.Set, extruders int) {
	declared, present := from[ArgNozzleTool]
	if !present || extruders < 1 {
		return
	}

	declared.Limit = float64(extruders - 1)
	declared.HasLimit = true
	into[ArgNozzleTool] = declared
}

// heaterBounds reads the heater sections out of the machine's configuration
// dump. configfile.settings keys are lowercase, so the chamber section is
// looked up by the lowercase object name. A section that is not there, or that
// this build cannot read, is absent rather than a refusal. The bounds narrow a
// descriptor, so failing to read one leaves the ceiling this driver already
// declared.
func heaterBounds(dumped configDump, chamber chamberRole) map[string]heaterConfig {
	sections := []string{heaterExtruder, heaterBed}
	if chamber.heater {
		sections = append(sections, strings.ToLower(chamber.object))
	}

	out := make(map[string]heaterConfig, len(sections))

	for _, section := range sections {
		raw, present := dumped.Settings[section]
		if !present {
			continue
		}

		var configured heaterConfig
		if json.Unmarshal(raw, &configured) != nil {
			continue
		}

		out[section] = configured
	}

	return out
}

// gcodeArgument is the descriptor of a raw gcode line.
func gcodeArgument() connectorapi.Value {
	return connectorapi.Value{
		Flag: true, Kind: connectorapi.KindString, Required: true, Limit: maxRawGCode, HasLimit: true,
	}
}

// forceGCodeArgument is gcodeArgument narrowed to the one word the force verb
// carries.
func forceGCodeArgument() connectorapi.Value {
	value := gcodeArgument()
	value.Supported = []string{emergencyWord}

	return value
}

const gcodeArgName = "gcode"

// methodFor is the JSON-RPC call one verb becomes, and ok is false for a verb
// this dialect sends as gcode instead.
func methodFor(verb connectorapi.Capability) (method string, ok bool) {
	switch verb {
	case VerbPausePrint:
		return methodPrintPause, true
	case VerbResumePrint:
		return methodPrintResume, true
	case VerbStopPrint:
		return methodPrintCancel, true
	case VerbStartPrint:
		return methodPrintStart, true
	case VerbResetPrinter:
		return methodPrinterRestart, true
	case VerbReset:
		return methodFirmwareRestart, true
	default:
		return "", false
	}
}

// restarts reports whether the verb restarts Klipper, which ends the
// connection the call was made on.
func restarts(verb connectorapi.Capability) bool {
	return verb == VerbResetPrinter || verb == VerbReset
}

// gcodeFor is the gcode line one command becomes, for the verbs Moonraker has
// no call of its own for.
//
// SET_HEATER_TEMPERATURE is Klipper's own command and returns as soon as the
// target is set, so a caller is not held for the minutes a heater takes.
func gcodeFor(cmd connectorapi.Command, chamber string) (string, error) {
	switch cmd.Verb {
	case VerbNozzleTarget:
		return nozzleScript(cmd)
	case VerbBedTarget:
		return heaterScript(heaterBed, cmd.Verb, cmd.Args[bedArgument])
	case VerbChamberTarget:
		return heaterScript(heaterName(chamber), cmd.Verb, cmd.Args[chamberArgument])
	case VerbMove:
		return moveScript(cmd.Args)
	case VerbMoveZ:
		return moveZScript(cmd.Args)
	case VerbHome:
		return homeScript(cmd.Args)
	case VerbDisableSteppers:
		return disableSteppersScript, nil
	default:
		return "", fmt.Errorf("moonraker: %s reaches no call and no gcode line", cmd.Verb)
	}
}

// heaterScript is one target as Klipper's own command spells it. The host
// refuses anything but a whole number against the descriptor, so a value that
// does not read as one is this driver being reached in a way it was not.
func heaterScript(heater string, verb connectorapi.Capability, target string) (string, error) {
	degrees, err := strconv.Atoi(target)
	if err != nil {
		return "", fmt.Errorf("moonraker: %s takes a whole number of degrees and was given %q", verb, target)
	}

	return "SET_HEATER_TEMPERATURE HEATER=" + heater + " TARGET=" + strconv.Itoa(degrees), nil
}

// nozzleScript is a nozzle target. With a head named, it sets that extruder's
// heater. Without one, it is M104, which Klipper applies to the extruder
// printing now (klippy/kinematics/extruder.py).
func nozzleScript(cmd connectorapi.Command) (string, error) {
	named, present := cmd.Args[toolArgument]
	if !present {
		degrees, err := strconv.Atoi(cmd.Args[nozzleArgument])
		if err != nil {
			return "", fmt.Errorf("moonraker: %s takes a whole number of degrees and was given %q",
				cmd.Verb, cmd.Args[nozzleArgument])
		}

		return "M104 S" + strconv.Itoa(degrees), nil
	}

	tool, err := strconv.Atoi(named)
	if err != nil || tool < 0 {
		return "", fmt.Errorf("moonraker: %s takes a head counted from 0 and was given %q", cmd.Verb, named)
	}

	return heaterScript(extruderName(tool), cmd.Verb, cmd.Args[nozzleArgument])
}
