package moonraker

import (
	"context"
	"testing"
	"time"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/require"
)

func TestCancelledCallWaitingForWriterIsNotSent(t *testing.T) {
	host, err := moonrakersim.Start(t.Context(), moonrakersim.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = host.Close() })
	d := NewDriver(time.Second)
	connected, err := d.Connect(t.Context(), connectorapi.Endpoint{Address: host.Address()}, d.Baseline())
	require.NoError(t, err)
	s := connected.(*Session)
	t.Cleanup(func() { _ = s.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, s.writing.lock(context.Background()))
	defer s.writing.unlock()
	finished := make(chan error, 1)
	go func() {
		_, _, callErr := s.call(ctx, methodGcodeScript, map[string]any{"script": "G28"})
		finished <- callErr
	}()
	require.Eventually(t, func() bool {
		found := false
		s.pending.Range(func(_, _ any) bool { found = true; return false })
		return found
	}, time.Second, time.Millisecond)
	cancel()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("cancelled call remained blocked on the write lock")
	}
	require.Never(t, func() bool { return len(host.CallTimes(methodGcodeScript)) != 0 }, 100*time.Millisecond, time.Millisecond)
}

func TestCancelledCommandDoesNotWaitForMotionLock(t *testing.T) {
	d := NewDriver(time.Second)
	s := newSession(nil, time.Second, liveness{}, nil)
	s.caps = d.Baseline()
	require.NoError(t, s.motion.lock(context.Background()))
	defer s.motion.unlock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := s.Do(ctx, connectorapi.Command{Verb: VerbHome})
		finished <- err
	}()
	cancel()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("cancelled command remained blocked on the motion lock")
	}
}
