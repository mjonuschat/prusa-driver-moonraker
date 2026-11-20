package moonraker_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func filesOf(t *testing.T, session connectorapi.Session) connectorapi.Filesystem {
	t.Helper()

	files, ok := connectorapi.Files(session)
	require.True(t, ok, "the session does not list files")

	return files
}

func entryNamed(t *testing.T, entries []connectorapi.File, path string) connectorapi.File {
	t.Helper()

	for _, entry := range entries {
		if entry.Path == path {
			return entry
		}
	}

	require.Failf(t, "entry missing", "%s not in %+v", path, entries)

	return connectorapi.File{}
}

func TestListOfTheRootIsTheStorages(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)
	before := len(machine.Calls())

	got, err := filesOf(t, session).List(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, connectorapi.File{Path: "", Folder: true}, got.Directory)
	require.Equal(t, []connectorapi.File{{Path: "gcodes", Name: "gcodes", Folder: true}}, got.Entries)
	require.Len(t, machine.Calls(), before)
}

func TestListOfAFolderMapsEntries(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	got, err := filesOf(t, session).List(t.Context(), "gcodes")
	require.NoError(t, err)
	require.Equal(t, "gcodes", got.Directory.Path)
	require.True(t, got.Directory.Folder)

	benchy := entryNamed(t, got.Entries, "gcodes/benchy.gcode")
	require.Equal(t, int64(1000), benchy.Size)
	require.Equal(t, int64(1700000000), benchy.Modified)
	require.Equal(t, "benchy.gcode", benchy.Name)
	require.Equal(t, "benchy.gcode", benchy.DisplayName)
	require.False(t, benchy.ReadOnly)
	require.False(t, benchy.Folder)

	require.True(t, entryNamed(t, got.Entries, "gcodes/models").Folder)
}

func TestListOfASubfolder(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	got, err := filesOf(t, session).List(t.Context(), "gcodes/models")
	require.NoError(t, err)
	require.Equal(t, "gcodes/models", got.Directory.Path)
	require.True(t, got.Directory.Folder)
	require.Equal(t, int64(2000), entryNamed(t, got.Entries, "gcodes/models/part.gcode").Size)
}

func TestListOfAFileReturnsItAsTheDirectory(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	got, err := filesOf(t, session).List(t.Context(), "gcodes/models/part.gcode")
	require.NoError(t, err)
	require.False(t, got.Directory.Folder)
	require.Equal(t, "gcodes/models/part.gcode", got.Directory.Path)
	require.Empty(t, got.Entries)
}

func TestListOfAMissingPathIsListingRefused(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	_, err := filesOf(t, session).List(t.Context(), "gcodes/nope")
	require.ErrorIs(t, err, connectorapi.ErrListingRefused)
	require.True(t, fault.HasCode(err, connectorapi.ListingRefused.Code()))
	require.Contains(t, err.Error(), "Directory does not exist")
}

func TestListRefusesAForeignStorageBeforeAnyCall(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)
	before := len(machine.Requests("server.files.get_directory"))

	for _, path := range []string{"config", "gcodes/../config"} {
		_, err := filesOf(t, session).List(t.Context(), path)
		require.Truef(t, fault.HasCode(err, connectorapi.PathNotAllowed.Code()), "%s: %v", path, err)
	}

	require.Len(t, machine.Requests("server.files.get_directory"), before)
}

func TestListKeepsAnAwkwardNameExactly(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)

	got, err := filesOf(t, session).List(t.Context(), "gcodes/odd name #1?.gcode")
	require.NoError(t, err)
	require.False(t, got.Directory.Folder)

	var seen bool

	for _, raw := range machine.Requests("server.files.get_directory") {
		var params struct {
			Path string `json:"path"`
		}

		require.NoError(t, json.Unmarshal(raw, &params))

		seen = seen || params.Path == "gcodes/odd name #1?.gcode"
	}

	require.True(t, seen, "the name did not reach the machine as typed")
}

func TestAFolderListingRefreshesFreeSpace(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)
	machine.SetFreeSpace(5)

	_, err := filesOf(t, session).List(t.Context(), "gcodes")
	require.NoError(t, err)

	storages := storagesOf(t, session)
	require.Len(t, storages, 1)
	require.Equal(t, int64(5), storages[0].FreeSpace)
}

func TestAListNamesNeitherADotFileNorADotFolder(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	got, err := filesOf(t, session).List(t.Context(), "gcodes")
	require.NoError(t, err)

	for _, entry := range got.Entries {
		require.NotContains(t, entry.Name, ".thumbs")
		require.NotEqual(t, ".hidden.gcode", entry.Name)
	}

	require.NotEmpty(t, got.Entries)
}
