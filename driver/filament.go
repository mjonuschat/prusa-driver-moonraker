package moonraker

import (
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Loading and unloading filament run a macro the machine already has. Klipper
// has no such commands of its own, and what a load does, including the
// temperature, belongs to that macro.
const (
	VerbLoadFilament   = connectorapi.Capability("core.command.load_filament")
	VerbUnloadFilament = connectorapi.Capability("core.command.unload_filament")

	macroObjectPrefix = "gcode_macro "
)

// filamentMacros is the macro each verb runs, or empty when the machine has
// none for it.
type filamentMacros struct {
	load, unload string
}

// filamentMacrosOf finds the macros in the objects a machine lists.
// LOAD_FILAMENT and UNLOAD_FILAMENT win over the Marlin codes M701 and M702, and
// each verb is decided on its own. Klipper reads a command in any case, so the
// macro is run under its upper-case name.
func filamentMacrosOf(available []string) filamentMacros {
	return filamentMacros{
		load:   firstMacro(available, "LOAD_FILAMENT", "M701"),
		unload: firstMacro(available, "UNLOAD_FILAMENT", "M702"),
	}
}

func firstMacro(available []string, names ...string) string {
	for _, name := range names {
		for _, object := range available {
			if strings.EqualFold(object, macroObjectPrefix+name) {
				return name
			}
		}
	}

	return ""
}

func filamentBaseline() connectorapi.Set {
	return connectorapi.Set{
		VerbLoadFilament:   {Flag: true},
		VerbUnloadFilament: {Flag: true},
	}
}

func carryFilament(into, from connectorapi.Set, macros filamentMacros) {
	if macros.load != "" {
		carry(into, from, VerbLoadFilament)
	}

	if macros.unload != "" {
		carry(into, from, VerbUnloadFilament)
	}
}

// scriptFor is the macro a verb runs on this machine.
func (m filamentMacros) scriptFor(verb connectorapi.Capability) string {
	if verb == VerbLoadFilament {
		return m.load
	}

	return m.unload
}
