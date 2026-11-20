package moonraker_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	callProxy  = "server.spoolman.proxy"
	callSpool  = "server.spoolman.get_spool_id"
	callStatus = "server.spoolman.status"
)

func spoolPath(member string) string {
	return connectorapi.ToolPath(connectorapi.HeadID(0), member)
}

func plaSpool(id int, colour string) moonrakersim.Spool {
	return moonrakersim.Spool{ID: id, Body: map[string]any{
		"id":             id,
		"filament":       map[string]any{"material": "PLA", "color_hex": colour},
		"initial_weight": 750.0,
		"used_weight":    50.0,
	}}
}

func spoolMachine(t *testing.T, rate, active int) *moonrakersim.Host {
	t.Helper()

	return startMachine(t, moonrakersim.Options{Spoolman: &moonrakersim.Spoolman{
		SyncRate: rate, Active: active,
		Spools: []moonrakersim.Spool{moonrakersim.ABSSpool(108), plaSpool(2, "FF0000"), plaSpool(3, "00FF00")},
	}})
}

func absValues() map[string]string {
	return map[string]string{
		spoolPath(connectorapi.MaterialAbbreviation): `"ABS"`,
		spoolPath(connectorapi.MaterialType):         `"ABS"`,
		spoolPath(connectorapi.MaterialColor):        `"#0020C2"`,
		spoolPath(connectorapi.MaterialFullWeight):   "1000",
		spoolPath(connectorapi.ConsumedWeight):       "313.13",
	}
}

func clearedValues() map[string]string {
	out := map[string]string{}
	for path := range absValues() {
		out[path] = "null"
	}

	return out
}

func plaValues(colour string) map[string]string {
	return map[string]string{
		spoolPath(connectorapi.MaterialAbbreviation): `"PLA"`,
		spoolPath(connectorapi.MaterialType):         `"PLA"`,
		spoolPath(connectorapi.MaterialColor):        `"#` + colour + `"`,
		spoolPath(connectorapi.MaterialFullWeight):   "750",
		spoolPath(connectorapi.ConsumedWeight):       "50",
	}
}

func TestTheActiveSpoolIsPublishedAtSessionStart(t *testing.T) {
	t.Parallel()

	session := connect(t, spoolMachine(t, 0, 108))

	collect(t, session, absValues())
}

func TestAChangedSpoolIsPublished(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 0, 108)
	session := connect(t, machine)
	collect(t, session, absValues())

	machine.SetActiveSpool(2)

	collect(t, session, plaValues("FF0000"))
}

func TestAClearedSpoolRemovesEveryPath(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 0, 108)
	session := connect(t, machine)
	collect(t, session, absValues())

	machine.SetActiveSpool(0)

	collect(t, session, clearedValues())
}

func TestTwoQuickChangesEndOnTheLaterSpool(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 0, 0)
	session := connect(t, machine)

	machine.SetActiveSpool(2)
	machine.SetActiveSpool(3)

	tree := collect(t, session, plaValues("00FF00"))

	for _, patch := range firstPatches(t, session) {
		tree[patch.Path] = string(patch.Value)
	}

	require.Equal(t, `"#00FF00"`, tree[spoolPath(connectorapi.MaterialColor)])
}

func TestAFailedProxyRemovesThePathsAndTheNextChangeWritesThemAgain(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 0, 108)
	session := connect(t, machine)
	collect(t, session, absValues())

	machine.Refuse(callProxy, "boom")
	machine.SetActiveSpool(2)

	collect(t, session, clearedValues())

	machine.Allow(callProxy)
	machine.SetActiveSpool(3)

	collect(t, session, plaValues("00FF00"))
}

func TestAMachineWithNoSpoolmanMakesNoSpoolmanCall(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	session := connect(t, machine)

	patches := firstPatches(t, session)

	for path := range absValues() {
		require.False(t, written(patches, path), path)
	}

	require.False(t, machine.Called(callSpool))
	require.False(t, machine.Called(callProxy))
	require.False(t, machine.Called(callStatus))
}

func TestAnUnansweredProxyRemovesThePathsAfterTheTimeout(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 0, 108)
	d := moonraker.NewDriver(300 * time.Millisecond)
	session := connectUnit(t, d, machine, "quiet")
	collect(t, session, absValues())

	machine.Silence(callProxy)
	machine.SetActiveSpool(2)

	collect(t, session, clearedValues())

	machine.Unsilence(callProxy)
	machine.SetActiveSpool(3)

	collect(t, session, plaValues("00FF00"))
}

func TestAMethodNotFoundAfterPublishingRemovesThePathsAndEndsTheFeature(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 0, 108)
	session := connect(t, machine)
	collect(t, session, absValues())

	machine.Remove(callProxy)
	machine.SetActiveSpool(2)

	collect(t, session, clearedValues())

	calls := len(machine.CallTimes(callProxy))

	machine.SetActiveSpool(3)
	firstPatches(t, session)

	require.Len(t, machine.CallTimes(callProxy), calls, "a feature that ended makes no further call")
}

func TestARedialRemovesASpoolThatIsNoLongerActive(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 0, 108)
	d := moonraker.NewDriver(0)

	first := connectUnit(t, d, machine, "bench")
	collect(t, first, absValues())
	require.NoError(t, first.Close())

	machine.SetActiveSpool(0)

	second := connectUnit(t, d, machine, "bench")
	patches := firstPatches(t, second)

	require.True(t, removed(patches, spoolPath(connectorapi.MaterialColor)))
}

func pushPrint(machine *moonrakersim.Host, state string) {
	machine.Push(map[string]map[string]any{"print_stats": {"state": state}})
}

func TestTheEndOfAPrintRefreshesTheSpoolAfterTheNextSync(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 1, 108)
	session := connect(t, machine)
	collect(t, session, absValues())

	pushPrint(machine, "printing")
	machine.QueueUsage(108, 25)

	ended := time.Now()

	pushPrint(machine, "complete")

	want := absValues()
	want[spoolPath(connectorapi.ConsumedWeight)] = strconv.FormatFloat(313.13+25, 'f', -1, 64)

	collect(t, session, want)

	times := machine.CallTimes(callProxy)
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(ended), time.Second, "the refresh waited for the sync tick")
}

func TestACancelledPrintAlsoRefreshes(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 1, 108)
	session := connect(t, machine)
	collect(t, session, absValues())

	pushPrint(machine, "printing")
	machine.QueueUsage(108, 5)
	pushPrint(machine, "cancelled")

	want := absValues()
	want[spoolPath(connectorapi.ConsumedWeight)] = strconv.FormatFloat(313.13+5, 'f', -1, 64)

	collect(t, session, want)
}

func TestAPrintThatIsStillRunningDoesNotRefresh(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 1, 108)
	session := connect(t, machine)
	collect(t, session, absValues())

	pushPrint(machine, "printing")
	time.Sleep(3 * time.Second)

	require.Len(t, machine.CallTimes(callProxy), 1)
	require.Empty(t, machine.CallTimes(callStatus))
	_ = session
}

func TestASilentStatusDuringTheSettleEndsItAndClearsTheSpool(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 1, 108)
	d := moonraker.NewDriver(300 * time.Millisecond)
	session := connectUnit(t, d, machine, "silent")
	collect(t, session, absValues())

	machine.Silence(callStatus)
	pushPrint(machine, "printing")
	pushPrint(machine, "complete")

	collect(t, session, clearedValues())

	time.Sleep(time.Second)
	require.Len(t, machine.CallTimes(callStatus), 1)
}

func TestClosingTheSessionWhileASettleIsPending(t *testing.T) {
	t.Parallel()

	machine := spoolMachine(t, 1, 108)
	session := connect(t, machine)
	collect(t, session, absValues())

	pushPrint(machine, "printing")
	pushPrint(machine, "complete")
	require.NoError(t, session.Close())

	calls := len(machine.Calls())

	time.Sleep(3 * time.Second)
	require.Len(t, machine.Calls(), calls)
}
