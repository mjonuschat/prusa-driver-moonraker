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

func lightMachine(t *testing.T, object string, status, settings map[string]any) (*moonrakersim.Host, connectorapi.Session) {
	t.Helper()

	full := moonrakersim.DefaultStatus()
	full[object] = status

	if settings != nil {
		full["configfile"] = map[string]any{"settings": settings}
	}

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), object), Status: full,
	})

	return machine, connect(t, machine)
}

func TestTheChamberLightVerbIsOfferedWithALight(t *testing.T) {
	t.Parallel()

	_, session := lightMachine(t, "output_pin caselight", map[string]any{"value": 0.3}, nil)
	caps, _ := session.Capabilities()

	require.Contains(t, caps, moonraker.VerbSetChamberLED)
	require.Contains(t, caps, moonraker.ArgChamberLED)

	declared := caps[moonraker.ArgChamberLED]
	assert.Equal(t, connectorapi.KindInteger, declared.Kind)
	assert.True(t, declared.Required)
	assert.Equal(t, connectorapi.UnitPercent, declared.Unit)
	assert.Equal(t, 0.0, declared.Min)
	assert.Equal(t, 100.0, limitOf(t, caps, moonraker.ArgChamberLED))

	plain := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})
	plainCaps, _ := connect(t, plain).Capabilities()
	require.NotContains(t, plainCaps, moonraker.VerbSetChamberLED)
}

func TestTheLightLevelIsReportedAsAPercentage(t *testing.T) {
	t.Parallel()

	_, session := lightMachine(t, "output_pin caselight", map[string]any{"value": 0.3}, nil)
	collect(t, session, map[string]string{"/chamber/led_intensity": "30"})
}

func TestSettingTheLightRunsSetPinInTheConfiguredScale(t *testing.T) {
	t.Parallel()

	machine, session := lightMachine(t, "output_pin caselight", map[string]any{"value": 0.0},
		map[string]any{"output_pin caselight": map[string]any{"pwm": true, "scale": 100.0}})

	result, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbSetChamberLED, Args: map[string]string{"chamber.led_intensity": "30"},
	})
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.Equal(t, []string{"SET_PIN PIN=caselight VALUE=30"}, machine.Scripts())
}

func TestSettingALedStripRunsSetLed(t *testing.T) {
	t.Parallel()

	machine, session := lightMachine(t, "neopixel chamber_led", map[string]any{"color_data": []any{}},
		map[string]any{"neopixel chamber_led": map[string]any{"color_order": "GRBW"}})

	result, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbSetChamberLED, Args: map[string]string{"chamber.led_intensity": "50"},
	})
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.Equal(t, []string{"SET_LED LED=chamber_led RED=0.5 GREEN=0.5 BLUE=0.5 WHITE=0.5"}, machine.Scripts())
}

func TestAKlipperRefusalOfTheLightIsReturnedAsItIs(t *testing.T) {
	t.Parallel()

	machine, session := lightMachine(t, "output_pin chamber_light", map[string]any{"value": 0.0}, nil)
	machine.RefuseScript("SET_PIN", "Invalid pin value")

	result, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbSetChamberLED, Args: map[string]string{"chamber.led_intensity": "40"},
	})
	require.NoError(t, err)
	assert.False(t, result.OK)
	assert.Equal(t, "Invalid pin value", result.MachineReason)
}

func TestTheLightIsSetInEveryStateButError(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)
	allowed := []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED"}

	for _, state := range []connectorapi.StateName{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED", "ERROR"} {
		require.Equalf(t, slices.Contains(allowed, state), set.Accepted(state, moonraker.VerbSetChamberLED), "in %s", state)
	}
}
