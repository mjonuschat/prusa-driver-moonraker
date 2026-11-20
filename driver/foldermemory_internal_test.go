package moonraker

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func TestReconnectWithMoreFoldersThanTheChangeQueue(t *testing.T) {
	host, err := moonrakersim.Start(t.Context(), moonrakersim.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = host.Close() })

	d := NewDriver(time.Second)
	for i := range filesChangedDepth + 10 {
		d.folders.remember("unit", fmt.Sprintf("gcodes/%d", i))
	}

	connected := make(chan connectorapi.Session, 1)
	go func() {
		session, _ := d.Connect(t.Context(), connectorapi.Endpoint{
			Address: host.Address(), Options: map[string]string{"unit_uid": "unit"},
		}, d.Baseline())
		connected <- session
	}()

	var session connectorapi.Session
	select {
	case session = <-connected:
		require.NotNil(t, session)
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect blocked before returning the session")
	}
	t.Cleanup(func() { _ = session.Close() })

	changes := session.(*Session).FilesChanged()
	seen := make(map[string]bool)
	for len(seen) < filesChangedDepth+11 {
		select {
		case change := <-changes:
			require.True(t, change.Rescan)
			seen[change.File.Path] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("received %d rescans", len(seen))
		}
	}
}

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
