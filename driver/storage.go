package moonraker

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// rootInfo is one file root as server.files.roots lists it.
type rootInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Permissions string `json:"permissions"`
}

// diskUsage is the disk_usage block of server.files.get_directory, in bytes.
type diskUsage struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Free  int64 `json:"free"`
}

// gcodesState is what the handshake learned about the gcodes root.
type gcodesState struct{ present, writable bool }

// gcodesStorage is the one storage this driver reports. Any permission but
// "rw" counts as read-only, and usage is nil when the machine gave none.
func gcodesStorage(roots []rootInfo, usage *diskUsage) (storage connectorapi.Storage, found bool) {
	index := slices.IndexFunc(roots, func(root rootInfo) bool { return root.Name == gcodesRoot })
	if index < 0 {
		return connectorapi.Storage{}, false
	}

	storage = connectorapi.Storage{
		Mountpoint: "/" + gcodesRoot,
		Name:       gcodesRoot,
		Type:       "Klipper",
		ReadOnly:   roots[index].Permissions != "rw",
	}

	if usage != nil {
		storage.FreeSpace = usage.Free
		storage.FreeSpaceKnown = true
	}

	return storage, true
}

// filesState is the storage list the session holds, under its own lock.
type filesState struct {
	mu       sync.Mutex
	storages []connectorapi.Storage
	stale    bool
}

// Storages is the storage list held from the handshake. After a file change it
// first reads the free space again.
func (s *Session) Storages(ctx context.Context) ([]connectorapi.Storage, error) {
	s.files.mu.Lock()
	stale := s.files.stale && len(s.files.storages) > 0
	s.files.mu.Unlock()

	if stale {
		reply, refusal, err := s.directory(ctx, gcodesRoot)
		if err == nil && refusal == nil && reply.DiskUsage != nil {
			s.setFreeSpace(*reply.DiskUsage)
		}
	}

	s.files.mu.Lock()
	defer s.files.mu.Unlock()

	return append([]connectorapi.Storage{}, s.files.storages...), nil
}

// setFreeSpace replaces the reported free space and clears the stale mark.
func (s *Session) setFreeSpace(usage diskUsage) {
	s.files.mu.Lock()
	defer s.files.mu.Unlock()

	for i := range s.files.storages {
		s.files.storages[i].FreeSpace = usage.Free
		s.files.storages[i].FreeSpaceKnown = true
	}

	s.files.stale = false
}

// readStorage asks for the roots and the gcodes disk usage and holds the
// result. A refusal leaves the matching part unknown. Refused roots mean no
// gcodes root, and a refused directory means unknown free space.
func (s *Session) readStorage(ctx context.Context) (gcodesState, error) {
	var roots []rootInfo
	if err := s.callSync(ctx, methodRoots, nil, &roots); err != nil {
		if errors.Is(err, ErrRefused) {
			return gcodesState{}, nil
		}

		return gcodesState{}, err
	}

	if _, found := gcodesStorage(roots, nil); !found {
		return gcodesState{}, nil
	}

	var listing struct {
		DiskUsage *diskUsage `json:"disk_usage"`
	}

	params := map[string]any{"path": gcodesRoot}
	if err := s.callSync(ctx, methodGetDirectory, params, &listing); err != nil && !errors.Is(err, ErrRefused) {
		return gcodesState{}, err
	}

	storage, _ := gcodesStorage(roots, listing.DiskUsage)

	s.files.mu.Lock()
	s.files.storages = []connectorapi.Storage{storage}
	s.files.mu.Unlock()

	return gcodesState{present: true, writable: !storage.ReadOnly}, nil
}
