package moonraker

import (
	"slices"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// VerbMeshBedLeveling is the Level bed button. On a Klipper machine it levels
// the gantry with the machine's own levelling command.
const VerbMeshBedLeveling = connectorapi.Capability("core.command.mesh_bed_leveling")

// The Klipper objects that say how a machine levels, and the command each one
// registers.
const (
	objectZTilt           = "z_tilt"
	objectQuadGantryLevel = "quad_gantry_level"

	zTiltScript           = "Z_TILT_ADJUST"
	quadGantryLevelScript = "QUAD_GANTRY_LEVEL"
	homeWait              = 60 * time.Second
	levelWait             = 5 * time.Minute
)

// levelCommandOf is the gcode command that levels this machine, from the
// objects it lists. A machine with both objects is levelled with Z_TILT_ADJUST,
// and both is true so the caller can say so. The command is empty when the
// machine lists neither.
func levelCommandOf(available []string) (command string, both bool) {
	tilt := slices.Contains(available, objectZTilt)
	gantry := slices.Contains(available, objectQuadGantryLevel)

	switch {
	case tilt:
		return zTiltScript, gantry
	case gantry:
		return quadGantryLevelScript, false
	default:
		return "", false
	}
}

// waitFor is how long a call for this verb is awaited. Klipper answers a gcode
// script when it has finished, and homing and levelling take longer than one
// ordinary call. A configured timeout longer than that wait is kept.
func waitFor(verb connectorapi.Capability, base time.Duration) time.Duration {
	switch verb {
	case VerbHome:
		return max(base, homeWait)
	case VerbMeshBedLeveling, VerbLoadFilament, VerbUnloadFilament:
		return max(base, levelWait)
	default:
		return base
	}
}
