package moonraker

import (
	"slices"
	"sync"
)

// folderMemory keeps, per machine, the folders the host has listed, so that a
// redial can ask the host to read them again. A change made while the link was
// down produced no event. The zero value is usable.
type folderMemory struct {
	mu     sync.Mutex
	byUnit map[string][]string
}

// remember records a folder as the newest one for the unit. A folder already
// held moves to the newest position. Nothing is evicted: a folder missing from
// the record would stay stale in the host after a redial. Only folders that
// listed successfully are recorded, so the record is bounded by the library.
func (m *folderMemory) remember(unit, folder string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.byUnit == nil {
		m.byUnit = make(map[string][]string)
	}

	held := m.byUnit[unit]
	if at := slices.Index(held, folder); at >= 0 {
		held = slices.Delete(held, at, at+1)
	}

	held = append(held, folder)
	m.byUnit[unit] = held
}

func (m *folderMemory) recorded(unit string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.byUnit[unit])
}
