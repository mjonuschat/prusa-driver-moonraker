package moonraker

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheChamberLightIsFoundByDefaultName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		listed []string
		want   string
	}{
		{"none", []string{"fan", "output_pin beeper"}, ""},
		{"a chamber light pin", []string{"output_pin chamber_light"}, "output_pin chamber_light"},
		{"a chamber led strip", []string{"neopixel chamber_led"}, "neopixel chamber_led"},
		{"a case light", []string{"output_pin caselight"}, "output_pin caselight"},
		{"a case light with an underscore", []string{"output_pin case_light"}, "output_pin case_light"},
		{"a chamber light wins over a case light", []string{"output_pin caselight", "led chamber_light"}, "led chamber_light"},
		{"case does not matter", []string{"output_pin CaseLight"}, "output_pin CaseLight"},
		{"a pin that only looks like one", []string{"output_pin chamber_lightbar"}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, resolveRoles(tc.listed, nil, roleOptions{}).light)
		})
	}
}

func TestTheChamberLightOptionOverridesAndTurnsItOff(t *testing.T) {
	t.Parallel()

	listed := []string{"output_pin caselight", "neopixel bar"}

	assert.Equal(t, "neopixel bar", resolveRoles(listed, nil, roleOptions{chamberLight: "bar"}).light)
	assert.Empty(t, resolveRoles(listed, nil, roleOptions{chamberLight: "none"}).light)

	got := resolveRoles(listed, nil, roleOptions{chamberLight: "missing"})
	assert.Empty(t, got.light)
	assert.Equal(t, []unresolved{{"chamber_light_object", "missing"}}, got.unresolved)

	assert.Equal(t, "x", roleOptionsOf(map[string]string{"chamber_light_object": "x"}).chamberLight)
}

func TestTheChamberLightIsSubscribed(t *testing.T) {
	t.Parallel()

	assert.Contains(t, resolveRoles([]string{"output_pin caselight"}, nil, roleOptions{}).objects(), "output_pin caselight")
}

func control(t *testing.T, object, settings string) lightControl {
	t.Helper()

	var dumped configDump
	require.NoError(t, json.Unmarshal([]byte(`{"settings": `+settings+`}`), &dumped))

	return lightControlOf(object, dumped)
}

func TestALightPinIsReadFromItsConfiguration(t *testing.T) {
	t.Parallel()

	pwm := control(t, "output_pin caselight", `{"output_pin caselight": {"pwm": true, "scale": 100.0}}`)
	assert.Equal(t, lightControl{pin: true, name: "caselight", pwm: true, scale: 100}, pwm)

	digital := control(t, "output_pin lamp", `{"output_pin lamp": {"pwm": false}}`)
	assert.Equal(t, lightControl{pin: true, name: "lamp", scale: 1}, digital)

	unknown := control(t, "output_pin lamp", `{}`)
	assert.Equal(t, lightControl{pin: true, name: "lamp", pwm: true, scale: 1}, unknown,
		"a configuration that cannot be read is taken as a plain PWM pin")
}

func TestALedStripIsReadFromItsColourOrder(t *testing.T) {
	t.Parallel()

	rgbw := control(t, "neopixel bar", `{"neopixel bar": {"color_order": "GRBW"}}`)
	assert.Equal(t, lightControl{name: "bar", white: true}, rgbw)

	rgb := control(t, "neopixel bar", `{"neopixel bar": {"color_order": "GRB"}}`)
	assert.Equal(t, lightControl{name: "bar"}, rgb)

	chains := control(t, "dotstar bar", `{"dotstar bar": {"color_order": ["GRB", "RGBW"]}}`)
	assert.True(t, chains.white)

	assert.Equal(t, lightControl{name: "bar"}, control(t, "led bar", `{}`))
}

func TestTheLightScriptsAreKlippersOwn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		control lightControl
		percent string
		want    string
	}{
		{"a pwm pin on a scale of 100", lightControl{pin: true, name: "caselight", pwm: true, scale: 100}, "30", "SET_PIN PIN=caselight VALUE=30"},
		{"a pwm pin on a scale of 1", lightControl{pin: true, name: "l", pwm: true, scale: 1}, "33", "SET_PIN PIN=l VALUE=0.33"},
		{"a pwm pin off", lightControl{pin: true, name: "l", pwm: true, scale: 1}, "0", "SET_PIN PIN=l VALUE=0"},
		{"a digital pin is on for any value", lightControl{pin: true, name: "l", scale: 1}, "1", "SET_PIN PIN=l VALUE=1"},
		{"a digital pin on a scale of 100", lightControl{pin: true, name: "l", scale: 100}, "50", "SET_PIN PIN=l VALUE=100"},
		{"a digital pin off", lightControl{pin: true, name: "l", scale: 100}, "0", "SET_PIN PIN=l VALUE=0"},
		{"an rgb strip", lightControl{name: "bar"}, "50", "SET_LED LED=bar RED=0.5 GREEN=0.5 BLUE=0.5"},
		{"an rgbw strip", lightControl{name: "bar", white: true}, "100", "SET_LED LED=bar RED=1 GREEN=1 BLUE=1 WHITE=1"},
		{"an rgb strip off", lightControl{name: "bar"}, "0", "SET_LED LED=bar RED=0 GREEN=0 BLUE=0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := lightScript(tc.control, map[string]string{"chamber.led_intensity": tc.percent})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTheLightScriptRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()

	for _, args := range []map[string]string{
		nil, {"chamber.led_intensity": ""}, {"chamber.led_intensity": "abc"}, {"chamber.led_intensity": "NaN"},
		{"chamber.led_intensity": "-1"}, {"chamber.led_intensity": "101"}, {"chamber.led_intensity": "5;M112"},
	} {
		_, err := lightScript(lightControl{name: "l", pwm: true, pin: true, scale: 1}, args)
		require.Errorf(t, err, "%v", args)
	}
}

func TestTheLightReadingIsAPercentage(t *testing.T) {
	t.Parallel()

	pin := &gauges{roles: roles{light: "output_pin caselight"}}
	expect(t, run(t, pin, `{"output_pin caselight": {"value": 0.3}}`), map[string]string{lightPath: "30"})
	expect(t, run(t, pin, `{"output_pin caselight": {"value": 0}}`), map[string]string{lightPath: "0"})

	strip := &gauges{roles: roles{light: "neopixel bar"}}
	expect(t, run(t, strip, `{"neopixel bar": {"color_data": [[0.5, 0.2, 0.1, 0.0], [1, 1, 1, 1]]}}`),
		map[string]string{lightPath: "50"})
	expect(t, run(t, strip, `{"neopixel bar": {"color_data": [[0, 0, 0]]}}`), map[string]string{lightPath: "0"})

	expect(t, run(t, strip, `{"neopixel bar": {"color_data": []}}`), map[string]string{})
	expect(t, run(t, &gauges{roles: roles{}}, `{"output_pin caselight": {"value": 0.3}}`), map[string]string{})
}
