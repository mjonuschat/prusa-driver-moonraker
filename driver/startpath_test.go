package moonraker_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func startPrintOf(t *testing.T, path string) (*moonrakersim.Host, error) {
	t.Helper()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)

	_, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbStartPrint, Args: map[string]string{"path": path},
	})

	return machine, err
}

func TestStartPrintStripsTheStoragePrefix(t *testing.T) {
	t.Parallel()

	machine, err := startPrintOf(t, "gcodes/models/part.gcode")
	require.NoError(t, err)
	require.Equal(t, []string{"models/part.gcode"}, machine.Filenames())
}

func TestStartPrintKeepsAnAwkwardNameExactly(t *testing.T) {
	t.Parallel()

	machine, err := startPrintOf(t, "gcodes/odd name #1?.gcode")
	require.NoError(t, err)
	require.Equal(t, []string{"odd name #1?.gcode"}, machine.Filenames())
}

func TestStartPrintRefusesPathsOffTheGcodesStorage(t *testing.T) {
	t.Parallel()

	for name, path := range map[string]string{
		"a bare path":     "models/part.gcode",
		"foreign storage": "config/printer.cfg",
		"an escape":       "gcodes/../config/printer.cfg",
		"the root":        "gcodes",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			machine, err := startPrintOf(t, path)
			require.True(t, fault.HasCode(err, connectorapi.PathNotAllowed.Code()), "%v", err)
			require.Empty(t, machine.Filenames())
		})
	}
}
