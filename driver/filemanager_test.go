package moonraker_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func managerOf(t *testing.T, session connectorapi.Session) connectorapi.FileManager {
	t.Helper()

	manager, ok := session.(connectorapi.FileManager)
	require.True(t, ok, "the session does not change files")

	return manager
}

func TestCreateFolderMakesIt(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	require.NoError(t, managerOf(t, session).CreateFolder(t.Context(), "gcodes/fresh"))

	got, err := filesOf(t, session).List(t.Context(), "gcodes")
	require.NoError(t, err)

	require.True(t, entryNamed(t, got.Entries, "gcodes/fresh").Folder)
}

func TestCreateFolderRefusesAnExistingPath(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	err := managerOf(t, session).CreateFolder(t.Context(), "gcodes/models")
	require.ErrorIs(t, err, connectorapi.ErrFileChangeRefused)
}

func TestDeleteFileRemovesIt(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	require.NoError(t, managerOf(t, session).DeleteFile(t.Context(), "gcodes/benchy.gcode"))

	got, err := filesOf(t, session).List(t.Context(), "gcodes")
	require.NoError(t, err)

	for _, entry := range got.Entries {
		require.NotEqual(t, "gcodes/benchy.gcode", entry.Path)
	}
}

func TestDeleteFileOnAFolderIsRefused(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	err := managerOf(t, session).DeleteFile(t.Context(), "gcodes/models")
	require.ErrorIs(t, err, connectorapi.ErrFileChangeRefused)
}

func TestDeleteFolderRefusesANonEmptyFolderAndSendsNoForce(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)

	err := managerOf(t, session).DeleteFolder(t.Context(), "gcodes/models")
	require.ErrorIs(t, err, connectorapi.ErrFileChangeRefused)

	requests := machine.Requests("server.files.delete_directory")
	require.NotEmpty(t, requests)

	for _, raw := range requests {
		var params map[string]any
		require.NoError(t, json.Unmarshal(raw, &params))
		require.NotContains(t, params, "force")
	}
}

func TestDeleteFolderRemovesAnEmptyFolder(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	require.NoError(t, managerOf(t, session).DeleteFolder(t.Context(), "gcodes/empty"))
}

func TestDeleteOfAFileInUseKeepsMoonrakersReason(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{
		Refused: map[string]string{"server.files.delete_file": "File currently in use"},
	}))

	err := managerOf(t, session).DeleteFile(t.Context(), "gcodes/benchy.gcode")
	require.ErrorIs(t, err, connectorapi.ErrFileChangeRefused)

	var refused *fault.Fault

	require.True(t, errors.As(err, &refused))

	reason, ok := refused.Params().Get("machine_reason")
	require.True(t, ok)
	require.Equal(t, "File currently in use", reason.String())
}

func TestTheRootIsNeverDeletable(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	manager := managerOf(t, connect(t, machine))

	for _, root := range []string{"gcodes", "gcodes/"} {
		require.True(t, fault.HasCode(manager.DeleteFile(t.Context(), root), connectorapi.PathNotAllowed.Code()))
		require.True(t, fault.HasCode(manager.DeleteFolder(t.Context(), root), connectorapi.PathNotAllowed.Code()))
	}

	require.Empty(t, machine.Requests("server.files.delete_file"))
	require.Empty(t, machine.Requests("server.files.delete_directory"))
}

func TestChangesRefuseEscapesBeforeAnyCall(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	manager := managerOf(t, connect(t, machine))

	for _, bad := range []string{"gcodes/../config/printer.cfg", "config/printer.cfg"} {
		code := connectorapi.PathNotAllowed.Code()
		require.True(t, fault.HasCode(manager.CreateFolder(t.Context(), bad), code))
		require.True(t, fault.HasCode(manager.DeleteFile(t.Context(), bad), code))
		require.True(t, fault.HasCode(manager.DeleteFolder(t.Context(), bad), code))
	}

	require.Empty(t, machine.Requests("server.files.post_directory"))
	require.Empty(t, machine.Requests("server.files.delete_file"))
	require.Empty(t, machine.Requests("server.files.delete_directory"))
}

func TestAReadOnlyGcodesRootHasNoManager(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{ReadOnlyGcodes: true})
	session := connect(t, machine)
	manager := managerOf(t, session)

	for _, verb := range []connectorapi.Capability{
		connectorapi.VerbCreateFolder, connectorapi.VerbDeleteFile, connectorapi.VerbDeleteFolder,
	} {
		require.False(t, offered(t, session, verb))
	}

	code := connectorapi.CapabilityNotSupported.Code()
	require.True(t, fault.HasCode(manager.CreateFolder(t.Context(), "gcodes/x"), code))
	require.True(t, fault.HasCode(manager.DeleteFile(t.Context(), "gcodes/x"), code))
	require.True(t, fault.HasCode(manager.DeleteFolder(t.Context(), "gcodes/x"), code))
	require.Empty(t, machine.Requests("server.files.post_directory"))
}
