package moonraker_test

import (
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	moonraker "github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func fanMachine(t *testing.T, speed float64) (*moonrakersim.Host, connectorapi.Session) {
	t.Helper()

	status := moonrakersim.DefaultStatus()
	status["fan_generic chamber_fan"] = map[string]any{"speed": speed, "rpm": nil}

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), "fan_generic chamber_fan"), Status: status,
	})

	return machine, connect(t, machine)
}

func TestTheChamberFanVerbIsOfferedWithAFan(t *testing.T) {
	t.Parallel()

	_, session := fanMachine(t, 0.4)
	caps, _ := session.Capabilities()

	require.Contains(t, caps, moonraker.VerbSetChamberFan)
	require.Contains(t, caps, moonraker.ArgChamberFan)

	declared := caps[moonraker.ArgChamberFan]
	assert.Equal(t, connectorapi.KindInteger, declared.Kind)
	assert.True(t, declared.Required)
	assert.Equal(t, connectorapi.UnitPercent, declared.Unit)
	assert.Equal(t, 0.0, declared.Min, "Klipper has no automatic mode to hand a fan back to")
	assert.Equal(t, 100.0, limitOf(t, caps, moonraker.ArgChamberFan))

	plain := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})
	plainCaps, _ := connect(t, plain).Capabilities()
	require.NotContains(t, plainCaps, moonraker.VerbSetChamberFan)
}

func TestTheChamberFanSpeedIsReportedToConnect(t *testing.T) {
	t.Parallel()

	_, session := fanMachine(t, 0.4)
	collect(t, session, map[string]string{"/chamber/fan_pwm_target": "40"})
}

func TestSettingTheChamberFanRunsSetFanSpeed(t *testing.T) {
	t.Parallel()

	machine, session := fanMachine(t, 0)

	for _, percent := range []string{"60", "0"} {
		result, err := session.Do(t.Context(), connectorapi.Command{
			Verb: moonraker.VerbSetChamberFan, Args: map[string]string{"chamber.fan_pwm_target": percent},
		})
		require.NoError(t, err)
		assert.True(t, result.OK)
	}

	assert.Equal(t, []string{"SET_FAN_SPEED FAN=chamber_fan SPEED=0.6", "SET_FAN_SPEED FAN=chamber_fan SPEED=0"},
		machine.Scripts())
}

func TestAKlipperRefusalOfTheChamberFanIsReturnedAsItIs(t *testing.T) {
	t.Parallel()

	machine, session := fanMachine(t, 0)
	machine.RefuseScript("SET_FAN_SPEED", "Unknown fan")

	result, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbSetChamberFan, Args: map[string]string{"chamber.fan_pwm_target": "50"},
	})
	require.NoError(t, err)
	assert.False(t, result.OK)
	assert.Equal(t, "Unknown fan", result.MachineReason)
}

func TestTheChamberFanIsSetInEveryStateButError(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)
	allowed := []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED"}

	for _, state := range []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED", "ERROR"} {
		require.Equalf(t, slices.Contains(allowed, state), set.Accepted(state, moonraker.VerbSetChamberFan), "in %s", state)
	}
}
