package moonraker_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestOneDriverServesSeveralMachinesAtOnce holds that one Driver is shared by
// every endpoint, so nothing one session holds may leak into another.
func TestOneDriverServesSeveralMachinesAtOnce(t *testing.T) {
	t.Parallel()

	const count = 4

	shared := moonraker.NewDriver(0)
	machines := make([]*moonrakersim.Host, count)
	sessions := make([]connectorapi.Session, count)
	errs := make([]error, count)

	for i := range machines {
		machines[i] = startMachine(t, moonrakersim.Options{Hostname: fmt.Sprintf("bench-%d", i)})
	}

	var wg sync.WaitGroup

	for i := range machines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			sessions[i], errs[i] = shared.Connect(
				t.Context(), connectorapi.Endpoint{Address: machines[i].Address()}, shared.Baseline())
		}()
	}

	wg.Wait()

	for i := range sessions {
		require.NoError(t, errs[i], "connect to machine %d", i)

		t.Cleanup(func() {
			if closeErr := sessions[i].Close(); closeErr != nil {
				t.Errorf("close session %d: %v", i, closeErr)
			}
		})
	}

	for i := range machines {
		temperature := 200 + i

		machines[i].Push(map[string]map[string]any{"extruder": {"temperature": float64(temperature)}})
	}

	for i := range sessions {
		collect(t, sessions[i], map[string]string{"/tools/0/nozzle_temp": fmt.Sprint(200 + i)})
		require.Equal(t, fmt.Sprintf("bench-%d", i), sessions[i].Identity().Model)
	}
}
