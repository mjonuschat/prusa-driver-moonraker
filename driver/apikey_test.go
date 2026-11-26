package moonraker_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const testAPIKey = "secret-0123456789abcdef"

// connectWithKey dials machine with the endpoint option api_key set to key.
func connectWithKey(t *testing.T, machine *moonrakersim.Host, key string) (connectorapi.Session, error) {
	t.Helper()

	d := moonraker.NewDriver(0)
	endpoint := connectorapi.Endpoint{Address: machine.Address(), Options: map[string]string{"api_key": key}}

	session, err := d.Connect(t.Context(), endpoint, d.Baseline())
	if session != nil {
		t.Cleanup(func() { _ = session.Close() })
	}

	return session, err
}

func TestAMoonrakerThatWantsAKeyTakesTheAPIKeyOption(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		APIKey: testAPIKey,
		Thumbnails: []moonrakersim.Thumbnail{{
			Width: 300, Height: 300, Size: len(pngBody),
			RelativePath: ".thumbs/benchy-300x300.png", Body: pngBody,
		}},
	})

	session, err := connectWithKey(t, machine, testAPIKey)
	require.NoError(t, err)

	got, err := filesOf(t, session).List(t.Context(), "gcodes/benchy.gcode")
	require.NoError(t, err)
	require.Equal(t, pngBody, got.Directory.Preview, "the thumbnail is fetched with the key")

	size := int64(mebibyte + 3)
	err = transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/keyed.gcode", Content: generatedContent(size),
	})
	require.NoError(t, err, "the upload is sent with the key")
	require.Equal(t, []moonrakersim.Upload{{Root: "gcodes", Name: "keyed.gcode", Size: size}}, machine.Uploads())
}

func TestAMissingKeySaysWhichOptionToSet(t *testing.T) {
	t.Parallel()

	_, err := connectWithKey(t, startMachine(t, moonrakersim.Options{APIKey: testAPIKey}), "")

	require.ErrorIs(t, err, moonraker.ErrUnauthorized)
	require.EqualError(t, err,
		"moonraker: authorization required: set the endpoint option api_key to a Moonraker API key")
}

func TestARefusedKeySaysTheKeyWasRefusedAndNeverCarriesIt(t *testing.T) {
	t.Parallel()

	_, err := connectWithKey(t, startMachine(t, moonrakersim.Options{APIKey: "another-key"}), testAPIKey)

	require.ErrorIs(t, err, moonraker.ErrUnauthorized)
	require.EqualError(t, err, "moonraker: authorization required: Moonraker refused the endpoint option api_key")

	var refused *moonraker.UnauthorizedError

	require.True(t, errors.As(err, &refused))
	require.True(t, refused.KeySent)
	walk(t, err)
}

func TestNoConnectErrorCarriesTheAPIKey(t *testing.T) {
	t.Parallel()

	for _, address := range []string{"127.0.0.1:1", "%zz", "ftp://host"} {
		t.Run(address, func(t *testing.T) {
			t.Parallel()

			d := moonraker.NewDriver(0)
			endpoint := connectorapi.Endpoint{Address: address, Options: map[string]string{"api_key": testAPIKey}}

			session, err := d.Connect(t.Context(), endpoint, d.Baseline())
			if session != nil {
				_ = session.Close()
			}

			require.Error(t, err)
			walk(t, err)
		})
	}
}
