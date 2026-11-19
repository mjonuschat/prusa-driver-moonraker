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

// TestOneDriverSharesFileStateAcrossSessions runs the file verbs from several
// sessions of one Driver at once, while the host reports file changes. It is
// meant to be run under -race.
func TestOneDriverSharesFileStateAcrossSessions(t *testing.T) {
	t.Parallel()

	const (
		sessionCount = 3
		rounds       = 5
	)

	machine := startMachine(t, moonrakersim.Options{})
	shared := moonraker.NewDriver(0)
	sessions := make([]connectorapi.Session, sessionCount)

	for i := range sessions {
		session, err := shared.Connect(
			t.Context(), connectorapi.Endpoint{Address: machine.Address()}, shared.Baseline())
		require.NoError(t, err)

		sessions[i] = session

		t.Cleanup(func() {
			if closeErr := session.Close(); closeErr != nil {
				t.Errorf("close session: %v", closeErr)
			}
		})
	}

	var wg sync.WaitGroup

	for i := range sessions {
		files := filesOf(t, sessions[i])
		manager := managerOf(t, sessions[i])

		wg.Add(1)

		go func() {
			defer wg.Done()

			for round := range rounds {
				folder := fmt.Sprintf("gcodes/race-%d-%d", i, round)

				if _, err := files.List(t.Context(), "gcodes"); err != nil {
					t.Errorf("list: %v", err)
				}

				if _, err := files.Storages(t.Context()); err != nil {
					t.Errorf("storages: %v", err)
				}

				if err := manager.CreateFolder(t.Context(), folder); err != nil {
					t.Errorf("create %s: %v", folder, err)

					continue
				}

				if err := manager.DeleteFolder(t.Context(), folder); err != nil {
					t.Errorf("delete %s: %v", folder, err)
				}
			}
		}()
	}

	wg.Add(1)

	go func() {
		defer wg.Done()

		for round := range rounds * sessionCount {
			machine.NotifyFiles(created("gcodes", fmt.Sprintf("note-%d.gcode", round)))
		}
	}()

	wg.Wait()
}
