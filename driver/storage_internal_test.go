package moonraker

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/require"
)

func TestFileChangeDuringStorageRefreshRemainsStale(t *testing.T) {
	host, err := moonrakersim.Start(t.Context(), moonrakersim.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = host.Close() })
	host.Silence(methodGetDirectory)

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+host.Address()+"/websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	s := newSession(conn, time.Second, liveness{}.orShipping(), slog.New(slog.DiscardHandler))
	s.files.storages = []connectorapi.Storage{{Name: gcodesRoot}}
	change := json.RawMessage(`[{"action":"create_file","item":{"root":"gcodes","path":"a.gcode"}}]`)
	s.filesChanged(change)

	finished := make(chan struct{})
	go func() {
		_, _ = s.Storages(t.Context())
		close(finished)
	}()
	require.Eventually(t, func() bool { return len(host.CallTimes(methodGetDirectory)) > 0 }, time.Second, time.Millisecond)
	s.filesChanged(change)
	s.answer(s.nextID.Load(), rpcFrame{Result: json.RawMessage(`{"disk_usage":{"free":100}}`)})
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("storage refresh did not finish")
	}

	s.files.mu.Lock()
	stale := s.files.stale
	s.files.mu.Unlock()
	require.True(t, stale, "a newer file change must trigger another refresh")

	refreshed := make(chan []connectorapi.Storage, 1)
	go func() {
		storages, _ := s.Storages(t.Context())
		refreshed <- storages
	}()
	require.Eventually(t, func() bool { return len(host.CallTimes(methodGetDirectory)) == 2 }, time.Second, time.Millisecond)
	s.answer(s.nextID.Load(), rpcFrame{Result: json.RawMessage(`{"disk_usage":{"free":50}}`)})
	select {
	case storages := <-refreshed:
		require.EqualValues(t, 50, storages[0].FreeSpace)
		require.True(t, storages[0].FreeSpaceKnown)
	case <-time.After(time.Second):
		t.Fatal("second storage refresh did not finish")
	}
}
