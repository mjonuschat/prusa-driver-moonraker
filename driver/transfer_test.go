package moonraker_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const mebibyte = 1024 * 1024

func TestASessionIsATransfersSession(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	_, ok := connectorapi.Uploads(session)
	require.True(t, ok)
}

func TestAFileReachesTheMachineInItsFolder(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)

	size := int64(3*mebibyte + 17)
	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/models/new.gcode", Content: generatedContent(size),
	})
	require.NoError(t, err)
	require.Equal(t, []moonrakersim.Upload{
		{Root: "gcodes", Path: "models", Name: "new.gcode", Size: size},
	}, machine.Uploads())
}

func TestAFileAtTheRootSendsNoPathField(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)

	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/top.gcode", Content: bytesContent("G28\n"),
	})
	require.NoError(t, err)
	require.Equal(t, []moonrakersim.Upload{{Root: "gcodes", Path: "", Name: "top.gcode", Size: 4}}, machine.Uploads())
}

func TestProgressReportsEveryMegabyteAndTheEnd(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	size := int64(3*mebibyte + 17)

	var log progressLog

	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/big.gcode", Content: generatedContent(size), Progress: log.add,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(log.reports), 4)
	require.Equal(t, int64(0), log.reports[0].Sent)
	require.Equal(t, size, log.reports[0].Total)

	var previous int64

	for _, report := range log.reports {
		require.GreaterOrEqual(t, report.Sent, previous)
		require.LessOrEqual(t, report.Sent, report.Total)
		require.False(t, report.Known)

		previous = report.Sent
	}

	last := log.reports[len(log.reports)-1]
	require.Equal(t, last.Total, last.Sent)
	require.Empty(t, last.Warning)
}

func TestOddNamesReachTheMachineByteForByte(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"a b.gcode", "a#b?c.gcode", "100%.gcode", `say "hi".gcode`, "Überdruck-日本.gcode",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			machine := startMachine(t, moonrakersim.Options{})
			session := connect(t, machine)

			folder := name + "-dir"
			require.NoError(t, managerOf(t, session).CreateFolder(t.Context(), "gcodes/"+folder))

			err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
				Path: "gcodes/" + folder + "/" + name, Content: bytesContent("x"),
			})
			require.NoError(t, err)
			require.Equal(t, []moonrakersim.Upload{{Root: "gcodes", Path: folder, Name: name, Size: 1}}, machine.Uploads())
		})
	}
}

func TestAZeroByteFileIsSent(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)

	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/empty.gcode", Content: bytesContent(nil),
	})
	require.NoError(t, err)
	require.Equal(t, []moonrakersim.Upload{{Root: "gcodes", Name: "empty.gcode", Size: 0}}, machine.Uploads())
}

func TestASecondSendOfTheSameNameReplacesTheFile(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))

	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/benchy.gcode", Content: bytesContent("12345"),
	})
	require.NoError(t, err)

	got, err := filesOf(t, session).List(t.Context(), "gcodes")
	require.NoError(t, err)
	require.Equal(t, int64(5), entryNamed(t, got.Entries, "gcodes/benchy.gcode").Size)
}

func TestAPathOutsideGcodesSendsNothing(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)
	before := len(machine.Calls())

	for _, path := range []string{"config/printer.cfg", "gcodes/../config/printer.cfg", "gcodes", "gcodes/", ""} {
		err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
			Path: path, Content: bytesContent("x"),
		})
		require.Truef(t, fault.HasCode(err, connectorapi.PathNotAllowed.Code()), "%q: %v", path, err)
	}

	require.Empty(t, machine.Uploads())
	require.Len(t, machine.Calls(), before)
}

// Not parallel, because it reads process-wide allocation counters. Go runs it
// before the parallel tests resume.
//
//nolint:paralleltest // reads process-wide allocation counters
func TestAFileIsStreamedNotBuffered(t *testing.T) {
	session := connect(t, startMachine(t, moonrakersim.Options{}))

	var before, after runtime.MemStats

	runtime.ReadMemStats(&before)

	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/huge.gcode", Content: generatedContent(64 * mebibyte),
	})
	require.NoError(t, err)

	runtime.ReadMemStats(&after)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(16*mebibyte))
}

func TestAReadOnlyRootIsNotOfferedUpload(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{ReadOnlyGcodes: true}))

	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/x.gcode", Content: bytesContent("x"),
	})
	require.True(t, fault.HasCode(err, connectorapi.CapabilityNotSupported.Code()), "%v", err)
}

func faultOf(t *testing.T, err error) *fault.Fault {
	t.Helper()

	var f *fault.Fault

	require.True(t, errors.As(err, &f))

	return f
}

func param(t *testing.T, f *fault.Fault, key string) string {
	t.Helper()

	value, ok := f.Params().Get(key)
	require.True(t, ok, key)

	return value.String()
}

func TestARefusalIsTransferRefusedWithMoonrakersMessage(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{UploadRefusal: &moonrakersim.UploadRefusal{
		Status: 403, Body: `{"error":{"code":403,"message":"File is loaded"}}`,
	}})
	session := connect(t, machine)

	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/a.gcode", Content: bytesContent("G28\n"),
	})
	require.ErrorIs(t, err, connectorapi.ErrTransferRefused)
	require.Equal(t, "File is loaded", param(t, faultOf(t, err), "machine_reason"))
	require.Empty(t, machine.Uploads())
}

func TestARefusalWithoutAJSONBodyUsesTheStatusText(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"", "<html>no</html>"} {
		session := connect(t, startMachine(t, moonrakersim.Options{
			UploadRefusal: &moonrakersim.UploadRefusal{Status: 403, Body: body},
		}))

		err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
			Path: "gcodes/a.gcode", Content: bytesContent("G28\n"),
		})
		require.ErrorIs(t, err, connectorapi.ErrTransferRefused)
		require.Equal(t, "Forbidden", param(t, faultOf(t, err), "machine_reason"))
	}
}

func TestAServerErrorIsTransferFailed(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{
		UploadRefusal: &moonrakersim.UploadRefusal{Status: 500, Body: "boom"},
	}))

	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/a.gcode", Content: bytesContent("G28\n"),
	})
	require.ErrorIs(t, err, connectorapi.ErrTransferFailed)
}

func TestARedirectIsTransferFailed(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{
		UploadRefusal: &moonrakersim.UploadRefusal{Status: 302, Body: ""},
	}))

	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/a.gcode", Content: bytesContent("G28\n"),
	})
	require.ErrorIs(t, err, connectorapi.ErrTransferFailed)
}

func TestADroppedConnectionIsTransferFailed(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{UploadDropAfter: 4}))

	done := make(chan error, 1)

	go func() {
		done <- transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
			Path: "gcodes/a.gcode", Content: generatedContent(4 * mebibyte),
		})
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, connectorapi.ErrTransferFailed)
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return")
	}
}

func TestAFailingContentIsTransferFailed(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))
	transfers := transfersOf(t, session)

	for _, content := range []failingContent{{}, {openFails: true}} {
		err := transfers.Send(t.Context(), connectorapi.Upload{Path: "gcodes/a.gcode", Content: content})
		require.ErrorIs(t, err, connectorapi.ErrTransferFailed)
	}

	require.NoError(t, transfers.Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/b.gcode", Content: bytesContent("G28\n"),
	}))
}

func TestAShortFileOnTheStorageIsNotOnStorage(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{UploadStoresSize: 3}))

	err := transfersOf(t, session).Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/a.gcode", Content: bytesContent("0123456789"),
	})
	require.True(t, fault.HasCode(err, connectorapi.TransferNotOnStorage.Code()))

	failure := faultOf(t, err)
	require.Equal(t, "10", param(t, failure, "expected_size"))
	require.Equal(t, "3", param(t, failure, "found"))
}

func TestASecondSendDuringATransferIsBusy(t *testing.T) {
	t.Parallel()

	session := connect(t, startMachine(t, moonrakersim.Options{}))
	transfers := transfersOf(t, session)
	gate := newGatedContent()
	done := make(chan error, 1)

	go func() {
		done <- transfers.Send(t.Context(), connectorapi.Upload{Path: "gcodes/a.gcode", Content: gate})
	}()

	<-gate.reading

	err := transfers.Send(t.Context(), connectorapi.Upload{Path: "gcodes/b.gcode", Content: bytesContent("x")})
	require.ErrorIs(t, err, connectorapi.ErrTransferBusy)
	require.Equal(t, "gcodes/b.gcode", param(t, faultOf(t, err), "path"))

	close(gate.released)
	require.NoError(t, <-done)
}

func TestAnEarlyRefusalDoesNotWaitForAnOpenThatIsStillBlocked(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{UploadRefusal: &moonrakersim.UploadRefusal{
		Status: 403, Body: `{"error":{"message":"no"}}`,
	}})
	session := connect(t, machine)
	transfers := transfersOf(t, session)

	done := make(chan error, 1)

	go func() {
		done <- transfers.Send(t.Context(), connectorapi.Upload{Path: "gcodes/a.gcode", Content: blockedOpenContent{}})
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, connectorapi.ErrTransferRefused)
	case <-time.After(5 * time.Second):
		t.Fatal("Send waited for the blocked Open")
	}

	machine.ClearUploadRefusal()
	require.NoError(t, transfers.Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/a.gcode", Content: bytesContent("G28\n"),
	}))
}

func TestCancellingTheContextAbortsTheTransfer(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)
	transfers := transfersOf(t, session)
	gate := newGatedContent()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		done <- transfers.Send(ctx, connectorapi.Upload{Path: "gcodes/a.gcode", Content: gate})
	}()

	<-gate.reading
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return")
	}

	select {
	case <-gate.closed:
	default:
		t.Fatal("the reader was not closed")
	}

	require.Empty(t, machine.Uploads())
	require.NoError(t, transfers.Send(t.Context(), connectorapi.Upload{
		Path: "gcodes/b.gcode", Content: bytesContent("G28\n"),
	}))
}
