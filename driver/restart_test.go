package moonraker_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Klipper restarts by dropping the connection, so the hang-up is the answer.
func TestARestartThatDropsTheConnectionIsSuccess(t *testing.T) {
	t.Parallel()

	cases := map[connectorapi.Capability]string{
		moonraker.VerbResetPrinter: "printer.restart",
		moonraker.VerbReset:        "printer.firmware_restart",
	}

	for verb, method := range cases {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			machine := startMachine(t, moonrakersim.Options{})

			result, err := connect(t, machine).Do(t.Context(), connectorapi.Command{Verb: verb})

			require.NoError(t, err)
			require.True(t, result.OK)
			require.True(t, machine.Called(method))
		})
	}
}

func TestARestartTheMachineRefusesIsNotSuccess(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Refused: map[string]string{"printer.restart": "Internal error"},
	})

	result, err := connect(t, machine).Do(t.Context(), connectorapi.Command{Verb: moonraker.VerbResetPrinter})

	require.NoError(t, err)
	require.False(t, result.OK)
	require.Equal(t, "Internal error", result.MachineReason)
}

func TestAMachineAnsweringKlippyDisconnectedIsSuccessForARestart(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{Refused: map[string]string{
		"printer.restart":     "Klippy Disconnected",
		"printer.print.start": "Klippy Disconnected",
	}})
	session := connect(t, machine)

	result, err := session.Do(t.Context(), connectorapi.Command{Verb: moonraker.VerbResetPrinter})
	require.NoError(t, err)
	require.True(t, result.OK)

	result, err = session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbStartPrint, Args: map[string]string{"path": "a.gcode"},
	})
	require.NoError(t, err)
	require.False(t, result.OK)
}
