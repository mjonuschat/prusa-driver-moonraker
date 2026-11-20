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

func chamberStatus(object string) moonrakersim.Options {
	status := moonrakersim.DefaultStatus()
	status[object] = map[string]any{"temperature": 30.0, "target": 0.0}

	available := []string{"extruder", "heater_bed"}
	if object == "heater_chamber" {
		available = append(available, object)
	}

	status["heaters"] = map[string]any{"available_heaters": available}

	return moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), object, "heaters"),
		Status:  status,
	}
}

func firstPatches(t *testing.T, session connectorapi.Session) []connectorapi.StatePatch {
	t.Helper()

	var out []connectorapi.StatePatch

	for {
		select {
		case patch, open := <-session.State():
			if !open {
				return out
			}

			out = append(out, patch)
		case <-time.After(300 * time.Millisecond):
			return out
		}
	}
}

func removed(patches []connectorapi.StatePatch, path string) bool {
	for _, patch := range patches {
		if patch.Path == path && string(patch.Value) == "null" {
			return true
		}
	}

	return false
}

func written(patches []connectorapi.StatePatch, path string) bool {
	for _, patch := range patches {
		if patch.Path == path && string(patch.Value) != "null" {
			return true
		}
	}

	return false
}

func connectWith(
	t *testing.T, d *moonraker.Driver, host *moonrakersim.Host, options map[string]string,
) connectorapi.Session {
	t.Helper()

	session, err := d.Connect(t.Context(), connectorapi.Endpoint{Address: host.Address(), Options: options}, d.Baseline())
	require.NoError(t, err)

	t.Cleanup(func() { _ = session.Close() })

	return session
}

func TestARedialRemovesTheTargetOfAChamberThatBecameASensor(t *testing.T) {
	t.Parallel()

	d := moonraker.NewDriver(0)
	options := map[string]string{"unit_uid": "chamber"}

	first := connectWith(t, d, startMachine(t, chamberStatus("heater_chamber")), options)
	require.True(t, written(firstPatches(t, first), "/temperature/chamber_target"))
	require.NoError(t, first.Close())

	second := connectWith(t, d, startMachine(t, chamberStatus("temperature_sensor Chamber")), options)
	patches := firstPatches(t, second)

	require.True(t, removed(patches, "/temperature/chamber_target"))
	require.True(t, written(patches, "/temperature/chamber"))
	require.False(t, removed(patches, "/temperature/chamber"))
}

func TestARedialRemovesAnExtraThatWasDroppedFromTheOptions(t *testing.T) {
	t.Parallel()

	d := moonraker.NewDriver(0)
	opts := chamberStatus("temperature_sensor Box")
	opts.Objects = append(opts.Objects, "temperature_sensor Chamber")
	opts.Status["temperature_sensor Chamber"] = map[string]any{"temperature": 25.0}

	host := startMachine(t, opts)

	first := connectWith(t, d, host, map[string]string{"unit_uid": "extra", "extra_sensors": "temperature_sensor Box"})
	require.True(t, written(firstPatches(t, first), "/sensors/Box/temperature"))
	require.NoError(t, first.Close())

	second := connectWith(t, d, host, map[string]string{"unit_uid": "extra"})
	require.True(t, removed(firstPatches(t, second), "/sensors/Box/temperature"))
}

func TestAFreshDriverSweepsTheFixedPathsButNotAnExtra(t *testing.T) {
	t.Parallel()

	d := moonraker.NewDriver(0)
	session := connectWith(t, d, startMachine(t, chamberStatus("temperature_sensor Chamber")),
		map[string]string{"unit_uid": "fresh"})

	patches := firstPatches(t, session)
	require.True(t, removed(patches, "/temperature/chamber_target"))

	for _, patch := range patches {
		require.NotContains(t, patch.Path, "/sensors/")
	}
}
