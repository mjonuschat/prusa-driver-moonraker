package moonraker_test

import (
	"testing"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi/connectorapitest"
)

// TestContract runs the published conformance suite against this dialect.
//
// Connectable says the suite may dial the machine below. Declaring NoLoopback
// instead would turn off every assertion about the intersective ceiling, Do,
// State and Close, which is most of the suite.
//
// The machine is moonrakersim and not hardware, so what passes here is the
// client and the contract. Whether a field a real Moonraker sends is spelled
// the way this dialect reads it is not settled by any of it.
func TestContract(t *testing.T) {
	t.Parallel()

	connectorapitest.RunDriverContract(t, func(t *testing.T) connectorapitest.Subject {
		return connectorapitest.Subject{
			Driver:      moonraker.NewDriver(0),
			Endpoint:    connectorapi.Endpoint{Address: startMachine(t, moonrakersim.Options{}).Address()},
			Connectable: true,
			SafeVerb:    moonraker.VerbSendStateInfo,
		}
	})
}

// TestTheDialectIsRegistered proves the init function ran and the descriptor
// was accepted. A registration missing a mandatory field panics there, so a
// dialect that fails this one does not start a host either.
func TestTheDialectIsRegistered(t *testing.T) {
	t.Parallel()

	for _, descriptor := range connectorapi.Descriptors() {
		if descriptor.Name() == moonraker.Type.String() {
			return
		}
	}

	t.Fatalf("%s registered nothing, so no host can build it", moonraker.Type)
}

// startMachine runs one fake Moonraker host for the length of a case.
func startMachine(t *testing.T, opts moonrakersim.Options) *moonrakersim.Host {
	t.Helper()

	host, err := moonrakersim.Start(t.Context(), opts)
	if err != nil {
		t.Fatalf("start a fake Moonraker host: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := host.Close(); closeErr != nil {
			t.Errorf("close the fake Moonraker host: %v", closeErr)
		}
	})

	return host
}
