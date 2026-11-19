package moonraker

import (
	"encoding/json"
	"path"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// filesChangedDepth bounds how many file changes may wait for the host. A
// change past it is dropped, and the host's next listing of the folder is what
// catches up.
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
func onGcodes(item *filelistItem) bool { return item != nil && item.Root == gcodesRoot }

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

	s.files.mu.Lock()
	s.files.stale = true
	s.files.mu.Unlock()

	for _, change := range changes {
		select {
		case s.changes <- change:
		default:
			s.log.Debug("a file change was dropped because the host is not reading them", "path", change.File.Path)
		}
	}
}
