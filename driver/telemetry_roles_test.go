package moonraker_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/require"

	moonraker "github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func TestTheChamberAndFanObjectsAreSubscribedTo(t *testing.T) {
	t.Parallel()

	status := moonrakersim.DefaultStatus()
	status["temperature_sensor Chamber"] = map[string]any{"temperature": 26.4}
	status["fan"] = map[string]any{"speed": 0.5, "rpm": nil}
	status["heater_fan hotend_fan"] = map[string]any{"speed": 0}
	status["heaters"] = map[string]any{"available_heaters": []string{"extruder", "heater_bed"}}

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), "temperature_sensor Chamber", "fan", "heater_fan hotend_fan", "heaters"),
		Status:  status,
	})

	driver := moonraker.NewDriver(0)
	session, err := driver.Connect(t.Context(), connectorapi.Endpoint{Address: machine.Address()}, driver.Baseline())
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	collect(t, session, map[string]string{"/temperature/chamber": "26.4"})
}

func TestUkuReportsItsChamberAndBedFan(t *testing.T) {
	t.Parallel()

	status := moonrakersim.DefaultStatus()
	status["heater_generic chamber"] = map[string]any{"temperature": 31.5, "target": 0}
	status["fan_generic bed_fans"] = map[string]any{"speed": 0.25, "rpm": nil}
	status["fan"] = map[string]any{"speed": 0.5, "rpm": 3158.5}
	status["heaters"] = map[string]any{"available_heaters": []string{"extruder", "heater_bed", "heater_generic chamber"}}

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), "heater_generic chamber", "fan_generic bed_fans", "fan", "heaters"),
		Status:  status,
	})

	driver := moonraker.NewDriver(0)
	session, err := driver.Connect(t.Context(), connectorapi.Endpoint{Address: machine.Address()}, driver.Baseline())
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	collect(t, session, map[string]string{
		"/temperature/chamber":        "31.5",
		"/temperature/chamber_target": "0",
		"/fan/bed/speed":              "25",
		"/fan/print/speed":            "50",
		"/fan/print/rpm":              "3158.5",
	})
}
