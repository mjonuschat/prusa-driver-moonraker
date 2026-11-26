package moonraker_test

import (
	"testing"
	"time"

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

func TestAMachineAnsweringKlippyDisconnectedIsNotSuccessForARestart(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{Refused: map[string]string{
		"printer.restart": "Klippy Disconnected",
	}})

	result, err := connect(t, machine).Do(t.Context(), connectorapi.Command{Verb: moonraker.VerbResetPrinter})
	require.NoError(t, err)
	require.False(t, result.OK)
	require.Equal(t, "Klippy Disconnected", result.MachineReason)
}

// Moonraker answers a restart and stays connected while Klipper starts again.
// The machine reads BUSY until Klipper is ready, and the session subscribes
// again, so updates keep arriving after the restart.
func TestARestartBehindMoonrakerReadsBusyAndSubscribesAgain(t *testing.T) {
	t.Parallel()

	for verb, method := range map[connectorapi.Capability]string{
		moonraker.VerbResetPrinter: "printer.restart",
		moonraker.VerbReset:        "printer.firmware_restart",
	} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			machine := startMachine(t, moonrakersim.Options{
				RestartKeepsConnection: true,
				StartupFor:             300 * time.Millisecond,
			})
			session := connect(t, machine)

			collect(t, session, map[string]string{"/state": `"IDLE"`})

			result, err := session.Do(t.Context(), connectorapi.Command{Verb: verb})
			require.NoError(t, err)
			require.True(t, result.OK)

			collect(t, session, map[string]string{"/state": `"BUSY"`})
			collect(t, session, map[string]string{"/state": `"IDLE"`})

			require.Eventually(t, func() bool {
				return len(machine.CallTimes("printer.objects.subscribe")) == 2
			}, settle, 10*time.Millisecond)

			machine.Push(map[string]map[string]any{"heater_bed": {"temperature": 61.5}})
			collect(t, session, map[string]string{"/temperature/bed": "61.5"})
		})
	}
}

// A Klipper that comes back with other objects had its configuration changed.
// The session hangs up so the host dials it again and builds a fresh probe.
func TestAKlipperThatComesBackWithOtherObjectsIsDialledAgain(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		RestartKeepsConnection: true,
		StartupFor:             100 * time.Millisecond,
		ObjectsAfterRestart:    without(moonrakersim.DefaultObjects(), "heater_bed"),
	})
	session := connect(t, machine)

	collect(t, session, map[string]string{"/state": `"IDLE"`})

	result, err := session.Do(t.Context(), connectorapi.Command{Verb: moonraker.VerbResetPrinter})
	require.NoError(t, err)
	require.True(t, result.OK)

	deadline := time.After(settle)

	for {
		select {
		case _, open := <-session.State():
			if !open {
				require.Len(t, machine.CallTimes("printer.objects.subscribe"), 1)

				return
			}
		case <-deadline:
			t.Fatal("the session stayed connected to a machine whose objects changed")
		}
	}
}
