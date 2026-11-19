package moonraker_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// filesChangedOf is the file-change channel of a session. The method is not on
// connectorapi.Session, so the helper asserts for it.
func filesChangedOf(t *testing.T, session connectorapi.Session) <-chan connectorapi.FileChange {
	t.Helper()

	source, ok := session.(interface {
		FilesChanged() <-chan connectorapi.FileChange
	})
	require.True(t, ok, "the session does not report file changes")

	return source.FilesChanged()
}

func nextChange(t *testing.T, changes <-chan connectorapi.FileChange) connectorapi.FileChange {
	t.Helper()

	select {
	case change := <-changes:
		return change
	case <-time.After(settle):
		require.FailNow(t, "no file change arrived")
	}

	return connectorapi.FileChange{}
}

func created(root, path string) moonrakersim.FileEvent {
	return moonrakersim.FileEvent{
		Action: "create_file",
		Item:   &moonrakersim.FileItem{Root: root, Path: path, Size: 5, Modified: 1700000000},
	}
}

func TestAMainsailUploadReachesTheHost(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	changes := filesChangedOf(t, connect(t, machine))

	machine.NotifyFiles(created("gcodes", "up.gcode"))

	got := nextChange(t, changes)
	require.Equal(t, connectorapi.FileChange{File: connectorapi.File{
		Path: "gcodes/up.gcode", Name: "up.gcode", DisplayName: "up.gcode", Size: 5, Modified: 1700000000,
	}}, got)
}

func TestAForeignRootIsIgnored(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	changes := filesChangedOf(t, connect(t, machine))

	machine.NotifyFiles(created("config", "printer.cfg"))
	machine.NotifyFiles(created("gcodes", "second.gcode"))

	require.Equal(t, "gcodes/second.gcode", nextChange(t, changes).File.Path)
}

func TestAFullBufferDropsInsteadOfBlocking(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)

	events := make([]moonrakersim.FileEvent, 0, 562)
	for range 562 {
		events = append(events, created("gcodes", "bulk.gcode"))
	}

	machine.NotifyFiles(events...)
	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing"}})

	collect(t, session, map[string]string{"/state": `"PRINTING"`})
}

func TestAMalformedNotificationIsCountedAndSurvived(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)
	changes := filesChangedOf(t, session)

	machine.NotifyRaw(`{"jsonrpc":"2.0","method":"notify_filelist_changed","params":"nope"}`)
	machine.NotifyFiles(created("gcodes", "after.gcode"))

	require.Eventually(t, func() bool {
		counter, ok := session.(interface{ Refused() uint64 })

		return ok && counter.Refused() > 0
	}, settle, 10*time.Millisecond)
	require.Equal(t, "gcodes/after.gcode", nextChange(t, changes).File.Path)
}

func TestAFullBufferLeavesARescanOfGcodes(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	changes := filesChangedOf(t, connect(t, machine))

	for range 600 {
		machine.NotifyFiles(created("gcodes", "bulk.gcode"))
	}

	time.Sleep(300 * time.Millisecond)

	var sawRescan bool

	for len(changes) > 0 {
		change := <-changes
		sawRescan = sawRescan || (change.Rescan && change.File.Path == "gcodes")
	}

	require.True(t, sawRescan)
}

func TestStoragesRefreshAfterAFileChange(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)
	changes := filesChangedOf(t, session)

	machine.SetFreeSpace(7)
	machine.NotifyFiles(created("gcodes", "up.gcode"))
	nextChange(t, changes)

	require.Equal(t, int64(7), storagesOf(t, session)[0].FreeSpace)
}
