package moonraker

import (
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func TestTheSweepListsTheFixedPathsAndTheRememberedOnesMinusWhatWasWritten(t *testing.T) {
	t.Parallel()

	got := sweepPaths(1,
		[]string{"/sensors/box/temperature", "/fans/duct/speed"},
		map[string]struct{}{"/fans/duct/speed": {}, "/temperature/chamber": {}})

	for _, want := range []string{
		"/temperature/chamber_target", "/fan/print/speed", "/fan/heatbreak/rpm", "/fan/bed/speed",
		"/job/layer", "/job/layer_count", "/dialog/id", "/dialog/title", "/dialog/text",
		"/sensors/box/temperature",
	} {
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
