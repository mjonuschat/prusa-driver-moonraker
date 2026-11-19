package moonraker

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFolderMemoryKeepsEveryFolder(t *testing.T) {
	t.Parallel()

	var memory folderMemory

	for i := range 300 {
		memory.remember("u", fmt.Sprintf("gcodes/%d", i))
	}

	got := memory.recorded("u")
	require.Len(t, got, 300)
	require.Equal(t, "gcodes/0", got[0])
}

func TestRememberingAFolderAgainMovesItToTheNewest(t *testing.T) {
	t.Parallel()

	var memory folderMemory

	memory.remember("u", "gcodes")
	memory.remember("u", "gcodes/a")
	memory.remember("u", "gcodes")

	require.Equal(t, []string{"gcodes/a", "gcodes"}, memory.recorded("u"))
}

func TestFolderMemoryIsPerUnit(t *testing.T) {
	t.Parallel()

	var memory folderMemory

	memory.remember("one", "gcodes/a")

	require.Empty(t, memory.recorded("two"))
	require.Equal(t, []string{"gcodes/a"}, memory.recorded("one"))
}

func TestFolderMemoryIsSafeAcrossGoroutines(t *testing.T) {
	t.Parallel()

	var (
		memory folderMemory
		group  sync.WaitGroup
	)

	for i := range 8 {
		group.Add(1)

		go func() {
			defer group.Done()

			for j := range 200 {
				memory.remember("u", fmt.Sprintf("gcodes/%d/%d", i, j))
				_ = memory.recorded("u")
			}
		}()
	}

	group.Wait()
	require.Len(t, memory.recorded("u"), 8*200)
}
