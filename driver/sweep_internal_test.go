package moonraker

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/require"
)

func TestOpeningSnapshotRetainsEveryRememberedCleanup(t *testing.T) {
	host, err := moonrakersim.Start(t.Context(), moonrakersim.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = host.Close() })

	d := NewDriver(time.Second)
	remembered := make(map[string]struct{})
	for i := range 100 {
		remembered[fmt.Sprintf("/sensors/old-%d/temperature", i)] = struct{}{}
	}
	d.paths.remember("unit", remembered)
	connected, err := d.Connect(t.Context(), connectorapi.Endpoint{
		Address: host.Address(), Options: map[string]string{"unit_uid": "unit"},
	}, d.Baseline())
	require.NoError(t, err)
	s := connected.(*Session)
	t.Cleanup(func() { _ = s.Close() })

	seen := make(map[string]bool)
	deadline := time.After(2 * time.Second)
	for len(seen) < len(remembered) {
		select {
		case patch := <-s.State():
			if _, ok := remembered[patch.Path]; ok && string(patch.Value) == "null" {
				seen[patch.Path] = true
			}
		case <-deadline:
			t.Fatalf("received %d of %d cleanup patches; dropped %d", len(seen), len(remembered), s.Dropped())
		}
	}
}

func TestTheSweepListsTheFixedPathsAndTheRememberedOnesMinusWhatWasWritten(t *testing.T) {
	t.Parallel()

	got := sweepPaths(1,
		[]string{"/sensors/box/temperature", "/fans/duct/speed"},
		map[string]struct{}{"/fans/duct/speed": {}, "/temperature/chamber": {}})

	for _, want := range []string{
		"/temperature/chamber_target", "/fan/print/speed", "/fan/heatbreak/rpm", "/fan/bed/speed",
		"/job/layer", "/job/layer_count", "/job/time_remaining", "/dialog/id", "/dialog/title", "/dialog/text",
		"/sensors/box/temperature",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("the sweep lacks %s", want)
		}
	}

	for _, want := range spoolPaths {
		if !slices.Contains(got, want) {
			t.Errorf("the sweep lacks %s", want)
		}
	}

	for _, not := range []string{"/temperature/chamber", "/fans/duct/speed"} {
		if slices.Contains(got, not) {
			t.Errorf("the sweep removes %s, which the session wrote", not)
		}
	}

	if !slices.IsSorted(got) {
		t.Error("the sweep is not sorted")
	}
}

func TestTheSweepCoversTheRPMMirrorOfEveryHead(t *testing.T) {
	t.Parallel()

	got := sweepPaths(1, nil, nil)

	for head := range maxSweptHeads {
		id := connectorapi.HeadID(head)
		for _, member := range []string{connectorapi.PrintFanRPM, connectorapi.HeatbreakFanRPM} {
			if !slices.Contains(got, connectorapi.ToolPath(id, member)) {
				t.Errorf("the sweep lacks %s", connectorapi.ToolPath(id, member))
			}
		}
	}
}

func TestThePathMemoryKeepsTheLatestSetPerUnit(t *testing.T) {
	t.Parallel()

	var m pathMemory
	m.remember("a", map[string]struct{}{"/x": {}, "/b": {}})
	m.remember("b", map[string]struct{}{"/y": {}})

	if got := m.recorded("a"); !slices.Equal(got, []string{"/b", "/x"}) {
		t.Errorf("recorded = %v", got)
	}
}
