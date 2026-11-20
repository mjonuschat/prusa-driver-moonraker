package moonraker

import (
	"encoding/json"
	"maps"
	"slices"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// maxSweptHeads is the number of heads whose rpm mirror a session sweeps even
// when the machine now lists fewer. A fresh process cannot know how many heads
// the machine had before.
const maxSweptHeads = 16

// pathMemory keeps, per machine, the telemetry paths its last session wrote, so
// that the next session can remove what it no longer writes. It lives as long
// as the process, so a path named by an earlier configuration (an extra sensor
// or fan) is only swept across redials. The zero value is usable.
type pathMemory struct {
	mu     sync.Mutex
	byUnit map[string]map[string]struct{}
}

func (m *pathMemory) remember(unit string, paths map[string]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.byUnit == nil {
		m.byUnit = make(map[string]map[string]struct{})
	}

	m.byUnit[unit] = maps.Clone(paths)
}

func (m *pathMemory) recorded(unit string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Sorted(maps.Keys(m.byUnit[unit]))
}

// sweepPaths is every path a new session removes before its own reading: the
// fixed set of optional paths plus the remembered ones, minus what the session
// writes. Removing a node the host does not hold has no effect.
func sweepPaths(heads int, remembered []string, written map[string]struct{}) []string {
	all := map[string]struct{}{
		chamberPath: {}, chamberTargetPath: {},
		layerPath: {}, layerCountPath: {},
		// The contract reads any patch under /job/ as a reported print and wants
		// /job/id among them. A stale id is wrong to leave behind in any case, and
		// the opening reading writes the live one after the sweep.
		connectorapi.JobIDPath:            {},
		connectorapi.JobTimeRemainingPath: {},
		dialogIDPath:                      {}, dialogTitlePath: {}, dialogTextPath: {},
	}

	for _, path := range spoolPaths {
		all[path] = struct{}{}
	}

	for _, role := range []string{"print", "heatbreak", "bed"} {
		all[fanPathPrefix+role+"/speed"] = struct{}{}
		all[fanPathPrefix+role+"/rpm"] = struct{}{}
	}

	for head := range max(heads, maxSweptHeads) {
		id := connectorapi.HeadID(head)
		all[connectorapi.ToolPath(id, connectorapi.PrintFanRPM)] = struct{}{}
		all[connectorapi.ToolPath(id, connectorapi.HeatbreakFanRPM)] = struct{}{}
	}

	for _, path := range remembered {
		all[path] = struct{}{}
	}

	for path := range written {
		delete(all, path)
	}

	return slices.Sorted(maps.Keys(all))
}

func sweepPatches(paths []string) []connectorapi.StatePatch {
	out := make([]connectorapi.StatePatch, 0, len(paths))
	for _, path := range paths {
		out = append(out, connectorapi.StatePatch{Path: path, Value: json.RawMessage("null")})
	}

	return out
}
