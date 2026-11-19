package moonraker_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

type storageLister interface {
	Storages(context.Context) ([]connectorapi.Storage, error)
}

var fileVerbs = []connectorapi.Capability{
	connectorapi.VerbSendInfo, connectorapi.VerbSendFileInfo,
	connectorapi.VerbCreateFolder, connectorapi.VerbDeleteFile, connectorapi.VerbDeleteFolder,
}

func storagesOf(t *testing.T, session connectorapi.Session) []connectorapi.Storage {
	t.Helper()

	lister, ok := session.(storageLister)
	require.True(t, ok, "the session does not list storages")

	got, err := lister.Storages(t.Context())
	require.NoError(t, err)

	return got
}

func offered(t *testing.T, session connectorapi.Session, verb connectorapi.Capability) bool {
	t.Helper()

	caps, _ := session.Capabilities()

	return caps.Has(verb)
}

func TestAMachineWithAGcodesRootIsOfferedTheFileVerbs(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	for _, verb := range fileVerbs {
		require.Truef(t, offered(t, session, verb), "%s is not offered", verb)
	}
}

func TestAReadOnlyGcodesRootIsOfferedListingButNotChanges(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{ReadOnlyGcodes: true}))

	require.True(t, offered(t, session, connectorapi.VerbSendInfo))
	require.True(t, offered(t, session, connectorapi.VerbSendFileInfo))
	require.False(t, offered(t, session, connectorapi.VerbCreateFolder))
	require.False(t, offered(t, session, connectorapi.VerbDeleteFile))
	require.False(t, offered(t, session, connectorapi.VerbDeleteFolder))
	require.True(t, storagesOf(t, session)[0].ReadOnly)
}

func TestAMachineWithNoGcodesRootIsOfferedNoFileVerbs(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{NoGcodesRoot: true}))

	for _, verb := range fileVerbs {
		require.Falsef(t, offered(t, session, verb), "%s is offered", verb)
	}

	require.Empty(t, storagesOf(t, session))
}

func TestStoragesReportTheGcodesRootWithFreeSpace(t *testing.T) {
	t.Parallel()

	got := storagesOf(t, connect(t, startMachine(t, moonrakersim.Options{})))

	require.Equal(t, []connectorapi.Storage{{
		Mountpoint: "/gcodes", Name: "gcodes", Type: "Klipper",
		FreeSpace: 1 << 30, FreeSpaceKnown: true,
	}}, got)
}

func TestStoragesSendNothing(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)

	before := len(machine.Calls())

	storagesOf(t, session)
	storagesOf(t, session)

	require.Len(t, machine.Calls(), before)
}

func TestAMachineThatRefusesGetDirectoryStillConnects(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{
		Refused: map[string]string{"server.files.get_directory": "no"},
	}))

	got := storagesOf(t, session)
	require.Len(t, got, 1)
	require.False(t, got[0].FreeSpaceKnown)

	for _, verb := range fileVerbs {
		require.Truef(t, offered(t, session, verb), "%s is not offered", verb)
	}
}

func TestAMachineThatRefusesRootsStillConnectsWithNoFileVerbs(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{
		Refused: map[string]string{"server.files.roots": "no"},
	}))

	for _, verb := range fileVerbs {
		require.Falsef(t, offered(t, session, verb), "%s is offered", verb)
	}

	require.Empty(t, storagesOf(t, session))
}
