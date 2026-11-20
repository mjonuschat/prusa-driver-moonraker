package moonraker

import (
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func gcodesItem(path string) *filelistItem {
	return &filelistItem{Path: path, Root: "gcodes", Size: 12, Modified: 1700000000.5}
}

func TestChangesOfEachAction(t *testing.T) {
	t.Parallel()

	file := connectorapi.File{
		Path: "gcodes/a/part.gcode", Name: "part.gcode", DisplayName: "part.gcode",
		Size: 12, Modified: 1700000000,
	}
	folder := connectorapi.File{
		Path: "gcodes/a/sub", Name: "sub", DisplayName: "sub", Size: 12, Modified: 1700000000, Folder: true,
	}

	cases := map[string]struct {
		event filelistEvent
		want  []connectorapi.FileChange
	}{
		"create_file": {
			filelistEvent{Action: "create_file", Item: gcodesItem("a/part.gcode")},
			[]connectorapi.FileChange{{File: file}},
		},
		"modify_file": {
			filelistEvent{Action: "modify_file", Item: gcodesItem("a/part.gcode")},
			[]connectorapi.FileChange{{File: file}},
		},
		"create_dir": {
			filelistEvent{Action: "create_dir", Item: gcodesItem("a/sub")},
			[]connectorapi.FileChange{{File: folder}},
		},
		"delete_file": {
			filelistEvent{Action: "delete_file", Item: gcodesItem("a/part.gcode")},
			[]connectorapi.FileChange{{File: connectorapi.File{Path: "gcodes/a/part.gcode"}, Removed: true}},
		},
		"delete_dir": {
			filelistEvent{Action: "delete_dir", Item: gcodesItem("a/sub")},
			[]connectorapi.FileChange{{
				File: connectorapi.File{Path: "gcodes/a/sub", Folder: true}, Removed: true,
			}},
		},
		"move_file": {
			filelistEvent{Action: "move_file", Item: gcodesItem("a/part.gcode"), SourceItem: gcodesItem("b/old.gcode")},
			[]connectorapi.FileChange{
				{File: connectorapi.File{Path: "gcodes/b/old.gcode"}, Removed: true},
				{File: file},
			},
		},
		"move_dir": {
			filelistEvent{Action: "move_dir", Item: gcodesItem("a/sub"), SourceItem: gcodesItem("b/old")},
			[]connectorapi.FileChange{
				{File: connectorapi.File{Path: "gcodes/b/old", Folder: true}, Removed: true},
				{File: folder},
			},
		},
		"root_update": {
			filelistEvent{Action: "root_update", Item: &filelistItem{Root: "gcodes"}},
			[]connectorapi.FileChange{{
				File:   connectorapi.File{Path: "gcodes", Name: "gcodes", DisplayName: "gcodes", Folder: true},
				Rescan: true,
			}},
		},
		"zip_files": {
			filelistEvent{Action: "zip_files", Item: gcodesItem("a/b.gcode")},
			[]connectorapi.FileChange{{
				File:   connectorapi.File{Path: "gcodes/a", Name: "a", DisplayName: "a", Folder: true},
				Rescan: true,
			}},
		},
		"zip_files at the root": {
			filelistEvent{Action: "zip_files", Item: gcodesItem("b.gcode")},
			[]connectorapi.FileChange{{
				File:   connectorapi.File{Path: "gcodes", Name: "gcodes", DisplayName: "gcodes", Folder: true},
				Rescan: true,
			}},
		},
		"create_file in another root": {
			filelistEvent{Action: "create_file", Item: &filelistItem{Root: "config", Path: "printer.cfg"}},
			nil,
		},
		"root_update of another root": {
			filelistEvent{Action: "root_update", Item: &filelistItem{Root: "config"}},
			nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, changesOf(tc.event))
		})
	}
}

func TestAMoveFromGcodesToConfigEmitsOnlyARemoval(t *testing.T) {
	t.Parallel()

	got := changesOf(filelistEvent{
		Action:     "move_file",
		SourceItem: gcodesItem("a.gcode"),
		Item:       &filelistItem{Root: "config", Path: "a.gcode"},
	})
	require.Equal(t, []connectorapi.FileChange{
		{File: connectorapi.File{Path: "gcodes/a.gcode"}, Removed: true},
	}, got)
}

func TestAMoveFromConfigIntoGcodesEmitsOnlyTheNewEntry(t *testing.T) {
	t.Parallel()

	got := changesOf(filelistEvent{
		Action:     "move_file",
		SourceItem: &filelistItem{Root: "config", Path: "a.gcode"},
		Item:       gcodesItem("a.gcode"),
	})
	require.Len(t, got, 1)
	require.False(t, got[0].Removed)
	require.Equal(t, "gcodes/a.gcode", got[0].File.Path)
}

func TestAMoveBetweenOtherRootsEmitsNothing(t *testing.T) {
	t.Parallel()

	got := changesOf(filelistEvent{
		Action:     "move_file",
		SourceItem: &filelistItem{Root: "config", Path: "a.cfg"},
		Item:       &filelistItem{Root: "logs", Path: "a.cfg"},
	})
	require.Empty(t, got)
}

func TestAnUnknownActionEmitsNothing(t *testing.T) {
	t.Parallel()

	require.Empty(t, changesOf(filelistEvent{Action: "teleport_file", Item: gcodesItem("a.gcode")}))
}

func TestOverflowKeepsARescanOfEveryListedFolder(t *testing.T) {
	t.Parallel()

	var memory folderMemory

	const folders = 20

	for i := range folders {
		memory.remember("u", fmt.Sprintf("gcodes/%d", i))
	}

	session := &Session{
		unit: "u", folders: &memory, log: slog.New(slog.DiscardHandler),
		changes: make(chan connectorapi.FileChange, 2), done: make(chan struct{}),
	}
	t.Cleanup(func() { close(session.done) })

	session.overflow("gcodes/x")

	seen := map[string]bool{}

	for len(seen) < folders+1 {
		select {
		case change := <-session.changes:
			seen[change.File.Path] = true
		case <-time.After(2 * time.Second):
			require.FailNow(t, "rescans were dropped", "got %d", len(seen))
		}
	}
}

func TestAChangeInsideADotFolderOrOnADotFileEmitsNothing(t *testing.T) {
	t.Parallel()

	for _, item := range []string{".thumbs", ".thumbs/a-300x300.png", ".hidden.gcode", "a/.cache/x.gcode"} {
		for _, action := range []string{"create_file", "modify_file", "create_dir", "delete_file", "delete_dir"} {
			require.Empty(t, changesOf(filelistEvent{Action: action, Item: gcodesItem(item)}), "%s %s", action, item)
		}
	}
}

func TestAMoveIntoOrOutOfADotFolderEmitsOnlyTheVisibleEnd(t *testing.T) {
	t.Parallel()

	moved := changesOf(filelistEvent{
		Action: "move_file", SourceItem: gcodesItem(".hidden/a.gcode"), Item: gcodesItem("a.gcode"),
	})
	require.Len(t, moved, 1)
	require.False(t, moved[0].Removed)
	require.Equal(t, "gcodes/a.gcode", moved[0].File.Path)
}
