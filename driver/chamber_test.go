package moonraker_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi/connectorapitest"
	"github.com/stretchr/testify/require"

	moonraker "github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func chamberMachine(
	t *testing.T, object string, heater bool, settings map[string]any, options map[string]string,
) (*moonrakersim.Host, connectorapi.Session, *moonraker.Driver) {
	t.Helper()

	status := moonrakersim.DefaultStatus()
	status[object] = map[string]any{"temperature": 30.0, "target": 0.0}
	available := []string{"extruder", "heater_bed"}

	if heater {
		available = append(available, object)
	}

	status["heaters"] = map[string]any{"available_heaters": available}
	status["configfile"] = map[string]any{"settings": settings}

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), object, "heaters", "configfile", "fan"),
		Status:  status,
	})

	driver := moonraker.NewDriver(0)
	endpoint := connectorapi.Endpoint{Address: machine.Address(), Options: options}

	session, err := driver.Connect(t.Context(), endpoint, driver.Baseline())
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return machine, session, driver
}

func TestAChamberHeaterIsOfferedTheTargetVerbBoundedByItsMaxTemp(t *testing.T) {
	t.Parallel()

	_, session, _ := chamberMachine(t, "heater_chamber", true,
		map[string]any{"heater_chamber": map[string]any{"min_temp": 0.0, "max_temp": 80.0}}, nil)

	caps, _ := session.Capabilities()
	require.True(t, caps.Has(moonraker.VerbChamberTarget))

	limit, bounded := caps.Limit(moonraker.ArgChamberTarget)
	require.True(t, bounded)
	require.InDelta(t, 80.0, limit, 0)
}

func TestAHeaterWithNoMaxTempIsOfferedTheCeiling(t *testing.T) {
	t.Parallel()

	_, session, _ := chamberMachine(t, "heater_chamber", true, map[string]any{}, nil)

	caps, _ := session.Capabilities()
	limit, bounded := caps.Limit(moonraker.ArgChamberTarget)
	require.True(t, bounded)
	require.InDelta(t, 250.0, limit, 0)
}

func TestAChamberSensorIsOfferedNoVerb(t *testing.T) {
	t.Parallel()

	_, session, _ := chamberMachine(t, "temperature_sensor Chamber", false, map[string]any{}, nil)

	caps, _ := session.Capabilities()
	require.False(t, caps.Has(moonraker.VerbChamberTarget))
	require.NotContains(t, caps, moonraker.ArgChamberTarget)
}

func TestANonHeaterChamberOverrideIsOfferedNoVerb(t *testing.T) {
	t.Parallel()

	machine, session, _ := chamberMachine(t, "heater_chamber", true, map[string]any{},
		map[string]string{"chamber_object": "fan"})

	caps, _ := session.Capabilities()
	require.False(t, caps.Has(moonraker.VerbChamberTarget))
	require.Empty(t, machine.Scripts())
}

func TestTheBedCeilingIsNow250AndIsNarrowedToTheHeater(t *testing.T) {
	t.Parallel()

	for max, want := range map[float64]float64{300: 250, 120: 120} {
		_, session, _ := chamberMachine(t, "heater_chamber", true,
			map[string]any{"heater_bed": map[string]any{"max_temp": max}}, nil)

		caps, _ := session.Capabilities()
		limit, bounded := caps.Limit(moonraker.ArgBedTarget)
		require.True(t, bounded)
		require.InDelta(t, want, limit, 0, "max_temp %v", max)
	}
}

func setChamber(t *testing.T, session connectorapi.Session, degrees string) {
	t.Helper()

	caps, _ := session.Capabilities()
	cmd, err := caps.Validate(connectorapi.Command{
		Verb: moonraker.VerbChamberTarget,
		Args: map[string]string{"chamber.target_temp": degrees},
	})
	require.NoError(t, err)

	result, err := session.Do(t.Context(), cmd)
	require.NoError(t, err)
	require.True(t, result.OK)
}

func TestTheChamberTargetRunsSetHeaterTemperatureForTheResolvedHeater(t *testing.T) {
	t.Parallel()

	machine, session, _ := chamberMachine(t, "heater_chamber", true, map[string]any{}, nil)
	setChamber(t, session, "75")

	require.Contains(t, machine.Scripts(), "SET_HEATER_TEMPERATURE HEATER=heater_chamber TARGET=75")
}

func TestTheFallbackBedFansChamberSetsTheBedFansHeater(t *testing.T) {
	t.Parallel()

	status := moonrakersim.DefaultStatus()
	status["heater_generic bed_fans"] = map[string]any{"temperature": 30.0, "target": 0.0}
	status["heaters"] = map[string]any{"available_heaters": []string{"extruder", "heater_bed", "heater_generic bed_fans"}}
	status["configfile"] = map[string]any{"settings": map[string]any{
		"heater_generic bed_fans": map[string]any{"max_temp": 70.0},
	}}

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), "heater_generic bed_fans", "heaters", "configfile"),
		Status:  status,
	})

	session := connect(t, machine)

	caps, _ := session.Capabilities()
	limit, _ := caps.Limit(moonraker.ArgChamberTarget)
	require.InDelta(t, 70.0, limit, 0)

	setChamber(t, session, "40")
	require.Contains(t, machine.Scripts(), "SET_HEATER_TEMPERATURE HEATER=bed_fans TARGET=40")
}

func TestTheVerbIsAcceptedInEveryState(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)

	for _, state := range []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED", "ERROR"} {
		require.Truef(t, set.Accepted(state, moonraker.VerbChamberTarget), "%s", state)
	}
}

func TestTheHostDoesNotCapTheChamberAtTheVocabularyLimit(t *testing.T) {
	t.Parallel()

	_, session, driver := chamberMachine(t, "heater_chamber", true,
		map[string]any{"heater_chamber": map[string]any{"max_temp": 80.0}}, nil)

	resolved := connectorapitest.NewHost(nil).Resolve(driver.Baseline(), session)

	limit, bounded := resolved.Limit(moonraker.ArgChamberTarget)
	require.True(t, bounded)
	require.InDelta(t, 80.0, limit, 0, "the chamber limit was capped by the resolution")
}
