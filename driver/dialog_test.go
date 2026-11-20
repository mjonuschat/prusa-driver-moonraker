package moonraker_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	moonraker "github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

const runoutSensor = "filament_switch_sensor runout_sensor"

// runoutMachine has a filament sensor that reports filament, and is connected.
func runoutMachine(t *testing.T) (*moonrakersim.Host, connectorapi.Session) {
	t.Helper()

	status := moonrakersim.DefaultStatus()
	status[runoutSensor] = map[string]any{"filament_detected": true}

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), runoutSensor), Status: status,
	})

	return machine, connect(t, machine)
}

// runOut puts the machine in the given print state with the sensor empty and
// waits for the dialog to be raised.
func runOut(t *testing.T, machine *moonrakersim.Host, session connectorapi.Session, printState, word string) {
	t.Helper()

	machine.Push(map[string]map[string]any{"print_stats": {"state": printState, "filename": "a.gcode"}})
	machine.Push(map[string]map[string]any{runoutSensor: {"filament_detected": false}})
	collect(t, session, map[string]string{"/state": `"` + word + `"`, "/dialog/id": "1"})
}

func answer(t *testing.T, session connectorapi.Session, id, button string) (connectorapi.Result, error) {
	t.Helper()

	return session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbDialogAction, Args: map[string]string{"dialog_id": id, "button": button},
	})
}

func TestDialogActionIsOfferedOnlyWithAFilamentSensor(t *testing.T) {
	t.Parallel()

	_, session := runoutMachine(t)
	caps, _ := session.Capabilities()

	for _, key := range []connectorapi.Capability{
		moonraker.VerbDialogAction, moonraker.ArgDialogID, moonraker.ArgDialogButton,
	} {
		require.Contains(t, caps, key)
	}

	assert.Equal(t, connectorapi.KindInteger, caps[moonraker.ArgDialogID].Kind)
	assert.Equal(t, connectorapi.KindString, caps[moonraker.ArgDialogButton].Kind)

	plain := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})
	plainCaps, _ := connect(t, plain).Capabilities()
	require.NotContains(t, plainCaps, moonraker.VerbDialogAction)
}

func TestTheRunoutDialogOffersPauseUntilThePrintIsPaused(t *testing.T) {
	t.Parallel()

	machine, session := runoutMachine(t)
	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing", "filename": "a.gcode"}})
	machine.Push(map[string]map[string]any{runoutSensor: {"filament_detected": false}})

	collect(t, session, map[string]string{"/dialog/id": "1", "/dialog/buttons": `["Pause","Stop"]`})

	machine.Push(map[string]map[string]any{"print_stats": {"state": "paused"}})
	collect(t, session, map[string]string{"/dialog/buttons": `["Resume","Stop"]`})

	machine.Push(map[string]map[string]any{runoutSensor: {"filament_detected": true}})
	collect(t, session, map[string]string{"/dialog/id": "null", "/dialog/buttons": "null"})
}

func TestResumeAnswersTheDialogByResumingThePrint(t *testing.T) {
	t.Parallel()

	machine, session := runoutMachine(t)
	runOut(t, machine, session, "paused", "PAUSED")

	result, err := answer(t, session, "1", "Resume")
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.True(t, machine.Called("printer.print.resume"))
}

func TestStopAnswersTheDialogByCancellingThePrint(t *testing.T) {
	t.Parallel()

	machine, session := runoutMachine(t)
	runOut(t, machine, session, "paused", "PAUSED")

	result, err := answer(t, session, "1", "Stop")
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.True(t, machine.Called("printer.print.cancel"))
}

func TestPauseAnswersTheDialogByPausingARunningPrint(t *testing.T) {
	t.Parallel()

	machine, session := runoutMachine(t)
	runOut(t, machine, session, "printing", "PRINTING")

	result, err := answer(t, session, "1", "Pause")
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.True(t, machine.Called("printer.print.pause"))
}

func TestPauseIsRefusedOnAPrintThatIsAlreadyPaused(t *testing.T) {
	t.Parallel()

	machine, session := runoutMachine(t)
	runOut(t, machine, session, "paused", "PAUSED")

	_, err := answer(t, session, "1", "Pause")
	require.Error(t, err)
	require.True(t, fault.HasCode(err, fault.PrinterVerbNotAllowedInState.Code()))
	assert.False(t, machine.Called("printer.print.pause"))
}

func TestResumeIsRefusedWhenThePrintIsNotPaused(t *testing.T) {
	t.Parallel()

	machine, session := runoutMachine(t)
	runOut(t, machine, session, "printing", "PRINTING")

	_, err := answer(t, session, "1", "Resume")
	require.Error(t, err)
	require.True(t, fault.HasCode(err, fault.PrinterVerbNotAllowedInState.Code()))
	assert.False(t, machine.Called("printer.print.resume"))

	result, err := answer(t, session, "1", "Stop")
	require.NoError(t, err)
	assert.True(t, result.OK, "a print that is running can still be stopped")
}

func TestAnAnswerThatNamesNoOpenDialogOrButtonIsRefused(t *testing.T) {
	t.Parallel()

	machine, session := runoutMachine(t)
	runOut(t, machine, session, "paused", "PAUSED")

	_, err := answer(t, session, "2", "Resume")
	require.Error(t, err)
	require.True(t, fault.HasCode(err, fault.PrinterVerbNotAllowedInState.Code()), "another dialog's id")

	_, err = answer(t, session, "1", "Retry")
	require.Error(t, err)
	require.True(t, fault.HasCode(err, connectorapi.ArgumentMalformed.Code()), "a button that was not offered")

	assert.False(t, machine.Called("printer.print.resume"))
	assert.False(t, machine.Called("printer.print.cancel"))
}

func TestNoAnswerIsSentOnceTheDialogHasClosed(t *testing.T) {
	t.Parallel()

	machine, session := runoutMachine(t)
	runOut(t, machine, session, "paused", "PAUSED")

	machine.Push(map[string]map[string]any{runoutSensor: {"filament_detected": true}})
	collect(t, session, map[string]string{"/dialog/id": "null"})

	_, err := answer(t, session, "1", "Resume")
	require.Error(t, err)
	assert.False(t, machine.Called("printer.print.resume"))
}

func TestTheMachinesRefusalOfAResumeIsReturnedAsItIs(t *testing.T) {
	t.Parallel()

	machine, session := runoutMachine(t)
	runOut(t, machine, session, "paused", "PAUSED")
	machine.Refuse("printer.print.resume", "Resume aborted, no filament detected")

	result, err := answer(t, session, "1", "Resume")
	require.NoError(t, err)
	assert.False(t, result.OK)
	assert.Equal(t, "Resume aborted, no filament detected", result.MachineReason)
}

func TestDialogActionIsAcceptedWhilePrintingOrPaused(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)

	for state, want := range map[connectorapi.StateName]bool{
		"IDLE": false, "PRINTING": true, "PAUSED": true, "FINISHED": false, "STOPPED": false, "ERROR": false,
	} {
		require.Equalf(t, want, set.Accepted(state, moonraker.VerbDialogAction), "in %s", state)
	}
}
