package moonraker_test

import (
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
