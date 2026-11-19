package moonraker_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func connectUnit(
	t *testing.T, d *moonraker.Driver, host *moonrakersim.Host, unit string,
) connectorapi.Session {
	t.Helper()

	session, err := d.Connect(t.Context(), connectorapi.Endpoint{
		Address: host.Address(), Options: map[string]string{"unit_uid": unit},
	}, d.Baseline())
	require.NoError(t, err)

	t.Cleanup(func() { _ = session.Close() })

	return session
}

func requireNoChange(t *testing.T, changes <-chan connectorapi.FileChange) {
	t.Helper()

	select {
	case change := <-changes:
		require.FailNow(t, "unexpected file change", "%+v", change)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestTheFirstConnectionEmitsNoRescan(t *testing.T) {
	t.Parallel()

	d := moonraker.NewDriver(0)
	session := connectUnit(t, d, startMachine(t, moonrakersim.Options{}), "first")

	requireNoChange(t, filesChangedOf(t, session))
}

func TestAReconnectRescansGcodesAndEveryListedFolder(t *testing.T) {
	t.Parallel()

	d := moonraker.NewDriver(0)
	host := startMachine(t, moonrakersim.Options{})
	first := connectUnit(t, d, host, "bench")

	_, err := filesOf(t, first).List(t.Context(), "gcodes")
	require.NoError(t, err)

	_, err = filesOf(t, first).List(t.Context(), "gcodes/models")
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second := connectUnit(t, d, host, "bench")
	changes := filesChangedOf(t, second)

	for _, want := range []string{"gcodes", "gcodes/models"} {
		change := nextChange(t, changes)
		require.True(t, change.Rescan)
		require.True(t, change.File.Folder)
		require.Equal(t, want, change.File.Path)
	}

	requireNoChange(t, changes)
}

func TestTwoMachinesDoNotShareFolderMemory(t *testing.T) {
	t.Parallel()

	d := moonraker.NewDriver(0)
	host := startMachine(t, moonrakersim.Options{})
	one := connectUnit(t, d, host, "one")

	_, err := filesOf(t, one).List(t.Context(), "gcodes/models")
	require.NoError(t, err)

	two := connectUnit(t, d, host, "two")

	requireNoChange(t, filesChangedOf(t, two))
}
