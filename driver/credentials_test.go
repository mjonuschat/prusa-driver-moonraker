package moonraker_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func walk(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		return
	}

	require.NotContains(t, err.Error(), "secret")

	switch u := err.(type) { //nolint:errorlint // walking the chain by hand
	case interface{ Unwrap() []error }:
		for _, e := range u.Unwrap() {
			walk(t, e)
		}
	case interface{ Unwrap() error }:
		walk(t, u.Unwrap())
	}
}

func TestNoConnectErrorCarriesTheCredentials(t *testing.T) {
	t.Parallel()

	unauthorized := startMachine(t, moonrakersim.Options{UpgradeStatus: http.StatusUnauthorized})

	for _, address := range []string{
		"u:p@a@127.0.0.1:1",
		"user:secret@%zz",
		"user:secret@127.0.0.1:1",
		"user:secret@" + unauthorized.Address(),
		"ftp://user:secret@host",
		"http://user:secret@",
		"http://user:secret@%zz",
		"user:secret@host",
	} {
		t.Run(address, func(t *testing.T) {
			t.Parallel()

			d := moonraker.NewDriver(0)

			session, err := d.Connect(t.Context(), connectorapi.Endpoint{Address: address}, d.Baseline())
			if session != nil {
				_ = session.Close()
			}

			require.Error(t, err)
			walk(t, err)
		})
	}
}
