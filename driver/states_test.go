package moonraker_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestTheDeclaredStatesAreSoundAndSendable runs the check the host runs at
// start, and holds every accepted verb to one the driver declares.
func TestTheDeclaredStatesAreSoundAndSendable(t *testing.T) {
	t.Parallel()

	types, err := connectorapi.NewRegistry()
	require.NoError(t, err)

	registration, err := types.Lookup(moonraker.Type)
	require.NoError(t, err)

	set := registration.Dialect().States
	require.Empty(t, set.Problems())
	require.True(t, set.Guarded())

	baseline := (&moonraker.Driver{}).Baseline()

	for _, state := range set.States {
		for _, verb := range state.Accepts {
			require.Truef(t, baseline.Has(verb), "%s accepts %s, which the driver does not declare", state.Name, verb)
		}
	}

	require.False(t, set.Accepted("IDLE", moonraker.VerbPausePrint), "nothing is printing in IDLE")
	require.True(t, set.Accepted("PAUSED", moonraker.VerbResumePrint))
}

func declaredSet(t *testing.T) connectorapi.StateSet {
	t.Helper()

	types, err := connectorapi.NewRegistry()
	require.NoError(t, err)

	registration, err := types.Lookup(moonraker.Type)
	require.NoError(t, err)

	return registration.Dialect().States
}

func TestEveryNewVerbIsAcceptedInTheRightStates(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)
	all := []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED", "ERROR"}

	cases := []struct {
		verb connectorapi.Capability
		in   []connectorapi.StateName
	}{
		{moonraker.VerbStartPrint, []connectorapi.StateName{"IDLE", "FINISHED", "STOPPED"}},
		{moonraker.VerbResetPrinter, all},
		{moonraker.VerbReset, all},
		{moonraker.VerbSendGCode, all[:5]},
		{moonraker.VerbForceGCode, all},
		{connectorapi.VerbSendInfo, all},
		{connectorapi.VerbSendFileInfo, all},
		{connectorapi.VerbCreateFolder, all},
		{connectorapi.VerbDeleteFile, all},
		{connectorapi.VerbDeleteFolder, all},
		{connectorapi.VerbFileUpload, all},
		{moonraker.VerbMove, []connectorapi.StateName{"IDLE", "PAUSED", "FINISHED", "STOPPED"}},
		{moonraker.VerbMoveZ, []connectorapi.StateName{"IDLE", "PAUSED", "FINISHED", "STOPPED"}},
		{moonraker.VerbHome, []connectorapi.StateName{"IDLE", "PAUSED", "FINISHED", "STOPPED"}},
		{moonraker.VerbDisableSteppers, []connectorapi.StateName{"IDLE", "PAUSED", "FINISHED", "STOPPED"}},
		{moonraker.VerbMeshBedLeveling, []connectorapi.StateName{"IDLE", "FINISHED", "STOPPED"}},
		{moonraker.VerbSetIdle, []connectorapi.StateName{"FINISHED", "STOPPED"}},
	}

	for _, tc := range cases {
		for _, state := range all {
			require.Equalf(t, slices.Contains(tc.in, state), set.Accepted(state, tc.verb),
				"%s in %s", tc.verb, state)
		}
	}
}

func TestTheTransitionsCoverShutdownAndRecovery(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)
	declared := func(from, to connectorapi.StateName, verb connectorapi.Capability) bool {
		return slices.Contains(set.Transitions, connectorapi.Transition{From: from, To: to, Verb: verb})
	}

	for _, from := range []connectorapi.StateName{"IDLE", "FINISHED", "STOPPED"} {
		require.Truef(t, declared(from, "PRINTING", moonraker.VerbStartPrint), "%s to PRINTING", from)
	}

	for _, from := range []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED"} {
		require.Truef(t, declared(from, "ERROR", moonraker.VerbForceGCode), "%s to ERROR", from)
	}

	for _, from := range []connectorapi.StateName{"FINISHED", "STOPPED"} {
		require.Truef(t, declared(from, "IDLE", moonraker.VerbSetIdle), "%s to IDLE", from)
	}

	require.True(t, declared("ERROR", "IDLE", moonraker.VerbResetPrinter))
	require.True(t, declared("ERROR", "IDLE", moonraker.VerbReset))

	for _, to := range []connectorapi.StateName{"FINISHED", "STOPPED", "PAUSED"} {
		require.Truef(t, declared("ERROR", to, ""), "ERROR to %s", to)
	}

	require.True(t, declared("PRINTING", "ERROR", ""))
	require.True(t, declared("ERROR", "PRINTING", ""))
}

func TestEveryStateHasAnEntryAndAnExitInTheTransitions(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)

	for _, state := range set.States {
		entered, left := false, false

		for _, move := range set.Transitions {
			entered = entered || move.To == state.Name
			left = left || move.From == state.Name
		}

		require.Truef(t, entered, "nothing enters %s", state.Name)
		require.Truef(t, left, "nothing leaves %s", state.Name)
	}
}

// BUSY is Klipper starting. It takes what ERROR takes, and a restart reaches it
// from every state.
func TestBusyTakesWhatErrorTakesAndEveryStateReachesIt(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)

	for _, verb := range (&moonraker.Driver{}).Baseline().Keys() {
		require.Equalf(t, set.Accepted("ERROR", verb), set.Accepted("BUSY", verb), "%s in BUSY", verb)
	}

	declared := func(from, to connectorapi.StateName, verb connectorapi.Capability) bool {
		return slices.Contains(set.Transitions, connectorapi.Transition{From: from, To: to, Verb: verb})
	}

	for _, from := range []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED", "ERROR"} {
		require.Truef(t, declared(from, "BUSY", ""), "%s to BUSY", from)
		require.Truef(t, declared(from, "BUSY", moonraker.VerbResetPrinter), "%s to BUSY on reset_printer", from)
		require.Truef(t, declared(from, "BUSY", moonraker.VerbReset), "%s to BUSY on reset", from)
	}

	for _, to := range []connectorapi.StateName{"IDLE", "ERROR"} {
		require.Truef(t, declared("BUSY", to, ""), "BUSY to %s", to)
	}
}
