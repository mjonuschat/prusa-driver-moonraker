package moonraker_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func TestARunningPrintReportsItsTimeRemaining(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(), EstimatedTime: 1000,
	})
	session := connect(t, machine)

	machine.Push(map[string]map[string]any{
		"print_stats":    {"state": "printing", "filename": "models/part.gcode", "print_duration": 1.0},
		"display_status": {"progress": 0.5},
	})

	// The estimate is asked for after the first reading, so frames that follow it
	// are the ones that carry a time remaining.
	stop := make(chan struct{})

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 2; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				machine.Push(map[string]map[string]any{"print_stats": {"print_duration": float64(i)}})
			}
		}
	}()

	t.Cleanup(func() {
		close(stop)
		wg.Wait()
	})

	collect(t, session, map[string]string{"/job/time_remaining": "500"})
}

func TestAPrintWhoseFileHasNoEstimateReportsNoTimeRemaining(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})
	session := connect(t, machine)

	machine.Push(map[string]map[string]any{
		"print_stats":    {"state": "printing", "filename": "models/part.gcode", "print_duration": 1.0},
		"display_status": {"progress": 0.5},
	})
	machine.Push(map[string]map[string]any{"print_stats": {"print_duration": 2.0}})

	patches := session.State()
	deadline := time.After(300 * time.Millisecond)

	for {
		select {
		case patch := <-patches:
			if patch.Path == "/job/time_remaining" {
				require.JSONEq(t, "null", string(patch.Value))
			}
		case <-deadline:
			return
		}
	}
}
