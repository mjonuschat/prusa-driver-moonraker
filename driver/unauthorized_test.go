package moonraker_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func connectTo(t *testing.T, status int) error {
	t.Helper()

	machine := startMachine(t, moonrakersim.Options{UpgradeStatus: status})
	d := moonraker.NewDriver(0)

	session, err := d.Connect(t.Context(), connectorapi.Endpoint{Address: machine.Address()}, d.Baseline())
	if session != nil {
		_ = session.Close()
	}

	return err
}

func TestARefusedUpgradeSaysAuthorizationIsRequired(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			err := connectTo(t, status)

			require.EqualError(t, err,
				"moonraker: authorization required: set the endpoint option api_key to a Moonraker API key")
			require.ErrorIs(t, err, moonraker.ErrUnauthorized)
			require.NotErrorIs(t, err, moonraker.ErrUnreachable)

			var refused *moonraker.UnauthorizedError

			require.True(t, errors.As(err, &refused))
			require.Equal(t, status, refused.Status)
		})
	}
}

func TestAServerErrorOnTheUpgradeIsStillUnreachable(t *testing.T) {
	t.Parallel()

	err := connectTo(t, http.StatusInternalServerError)

	require.ErrorIs(t, err, moonraker.ErrUnreachable)
	require.NotErrorIs(t, err, moonraker.ErrUnauthorized)
}
