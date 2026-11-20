package moonraker

import (
	"encoding/json"
	"path"
	"slices"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// filesChangedDepth bounds how many file changes may wait for the host. A
// change past it replaces the queue with a rescan of the gcodes storage.
const filesChangedDepth = 512

var _ interface {
	FilesChanged() <-chan connectorapi.FileChange
} = (*Session)(nil)

type filelistItem struct {
	Path     string  `json:"path"`
	Root     string  `json:"root"`
	Size     int64   `json:"size"`
	Modified float64 `json:"modified"`
}

// filelistEvent is the first element of a notify_filelist_changed frame.
type filelistEvent struct {
	Action     string        `json:"action"`
	Item       *filelistItem `json:"item"`
	SourceItem *filelistItem `json:"source_item"`
}

// FilesChanged is what the machine announced changing in its gcodes storage.
// It is never closed.
func (s *Session) FilesChanged() <-chan connectorapi.FileChange { return s.changes }

// onGcodes reports whether the item is in the gcodes root, the only root this
// driver exposes.
func onGcodes(item *filelistItem) bool {
	return item != nil && item.Root == gcodesRoot && !hidden(item.Path)
}

func hostPath(item *filelistItem) string { return gcodesRoot + "/" + item.Path }

func entryOf(item *filelistItem, folder bool) connectorapi.FileChange {
	name := path.Base(item.Path)

	return connectorapi.FileChange{File: connectorapi.File{
		Path: hostPath(item), Name: name, DisplayName: name, Folder: folder,
		Size: item.Size, Modified: int64(item.Modified),
	}}
}

func removalOf(item *filelistItem, folder bool) connectorapi.FileChange {
	return connectorapi.FileChange{
		File:    connectorapi.File{Path: hostPath(item), Folder: folder},
		Removed: true,
	}
}

func rescanOf(folder string) connectorapi.FileChange {
	name := path.Base(folder)

	return connectorapi.FileChange{
		File:   connectorapi.File{Path: folder, Name: name, DisplayName: name, Folder: true},
		Rescan: true,
	}
}

// changesOf is what one event means for the gcodes storage. Each end of a move
// is judged on its own root, so a move out of gcodes is a removal and a move
// in is a new entry.
func changesOf(event filelistEvent) []connectorapi.FileChange {
	var out []connectorapi.FileChange

	switch event.Action {
	case "create_file", "modify_file", "create_dir":
		if onGcodes(event.Item) {
			out = append(out, entryOf(event.Item, event.Action == "create_dir"))
		}
	case "delete_file", "delete_dir":
		if onGcodes(event.Item) {
			out = append(out, removalOf(event.Item, event.Action == "delete_dir"))
		}
	case "move_file", "move_dir":
		folder := event.Action == "move_dir"

		if onGcodes(event.SourceItem) {
			out = append(out, removalOf(event.SourceItem, folder))
		}

		if onGcodes(event.Item) {
			out = append(out, entryOf(event.Item, folder))
		}
	case "root_update":
		if onGcodes(event.Item) {
			out = append(out, rescanOf(gcodesRoot))
		}
	case "zip_files":
		if onGcodes(event.Item) {
			out = append(out, rescanOf(path.Join(gcodesRoot, path.Dir(event.Item.Path))))
		}
	}

	return out
}

// filesChanged turns one notify_filelist_changed notification into queued
// changes. Its params are an array whose first element is the event.
func (s *Session) filesChanged(params json.RawMessage) {
	var carried []json.RawMessage
	if json.Unmarshal(params, &carried) != nil || len(carried) == 0 {
		s.refused.Add(1)

		return
	}

	var event filelistEvent
	if json.Unmarshal(carried[0], &event) != nil {
		s.refused.Add(1)

		return
	}

	changes := changesOf(event)
	if len(changes) == 0 {
		return
	}

	// The host rescans one folder, not its descendants, so a root-wide change
	// also needs every folder it has listed.
	if event.Action == "root_update" && s.folders != nil {
		for _, folder := range s.folders.recorded(s.unit) {
			if folder != gcodesRoot {
				changes = append(changes, rescanOf(folder))
			}
		}
	}

	s.files.mu.Lock()
	s.files.stale = true
	s.files.mu.Unlock()

	for _, change := range changes {
		select {
		case s.changes <- change:
		default:
			s.overflow(change.File.Path)
		}
	}
}

// overflow runs when the queue is full. The queued changes are discarded and
// a rescan of gcodes and of every folder the host has listed takes their place, so the host
// relists what it missed instead of keeping a wrong tree.
func (s *Session) overflow(path string) {
	s.log.Debug("the file change queue is full, asking the host to rescan", "path", path)

	for {
		select {
		case <-s.changes:
			continue
		default:
		}

		break
	}

	s.queueRescan(gcodesRoot)

	if s.folders == nil {
		return
	}

	for _, folder := range s.folders.recorded(s.unit) {
		if folder != gcodesRoot {
			s.queueRescan(folder)
		}
	}
}

// queueRescan queues a rescan without ever dropping it. When the channel is
// full the folder waits in an unbounded set that a pump goroutine hands to the
// channel as the host reads, so no listed folder keeps stale contents.
func (s *Session) queueRescan(folder string) {
	select {
	case s.changes <- rescanOf(folder):
		return
	default:
	}

	s.rescans.mu.Lock()
	defer s.rescans.mu.Unlock()

	if slices.Contains(s.rescans.waiting, folder) {
		return
	}

	s.rescans.waiting = append(s.rescans.waiting, folder)

	if !s.rescans.pumping {
		s.rescans.pumping = true

		go s.pumpRescans()
	}
}

func (s *Session) pumpRescans() {
	for {
		s.rescans.mu.Lock()
		if len(s.rescans.waiting) == 0 {
			s.rescans.pumping = false
			s.rescans.mu.Unlock()

			return
		}

		folder := s.rescans.waiting[0]
		s.rescans.waiting = s.rescans.waiting[1:]
		s.rescans.mu.Unlock()

		select {
		case s.changes <- rescanOf(folder):
		case <-s.done:
			return
		}
	}
}

// rescanQueue holds rescans that did not fit in the change channel.
type rescanQueue struct {
	mu      sync.Mutex
	waiting []string
	pumping bool
}
