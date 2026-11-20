package moonraker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheChamberFanIsFoundByDefaultName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		listed []string
		want   string
	}{
		{"none", []string{"fan", "fan_generic bed_fans"}, ""},
		{"a chamber fan", []string{"fan_generic chamber_fan"}, "fan_generic chamber_fan"},
		{"an exhaust fan", []string{"fan_generic exhaust_fan"}, "fan_generic exhaust_fan"},
		{"a nevermore", []string{"fan_generic nevermore"}, "fan_generic nevermore"},
		{"chamber wins over exhaust", []string{"fan_generic exhaust_fan", "fan_generic chamber_fan"}, "fan_generic chamber_fan"},
		{"case does not matter", []string{"fan_generic Chamber_Fan"}, "fan_generic Chamber_Fan"},
		{"only a settable fan counts", []string{"heater_fan chamber_fan", "controller_fan chamber_fan"}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, resolveRoles(tc.listed, nil, roleOptions{}).chamberFan)
		})
	}
}

func TestTheChamberFanOptionOverridesAndTurnsItOff(t *testing.T) {
	t.Parallel()

	listed := []string{"fan_generic chamber_fan", "fan_generic filter"}

	assert.Equal(t, "fan_generic filter", resolveRoles(listed, nil, roleOptions{chamberFan: "filter"}).chamberFan)
	assert.Empty(t, resolveRoles(listed, nil, roleOptions{chamberFan: "none"}).chamberFan)

	got := resolveRoles(listed, nil, roleOptions{chamberFan: "missing"})
	assert.Empty(t, got.chamberFan)
	assert.Equal(t, []unresolved{{"chamber_fan_object", "missing"}}, got.unresolved)

	assert.Equal(t, "x", roleOptionsOf(map[string]string{"chamber_fan_object": "x"}).chamberFan)
	assert.Contains(t, resolveRoles(listed, nil, roleOptions{}).objects(), "fan_generic chamber_fan")
}

func TestTheChamberFanScriptIsKlippersOwn(t *testing.T) {
	t.Parallel()

	for percent, want := range map[string]string{
		"0":   "SET_FAN_SPEED FAN=chamber_fan SPEED=0",
		"40":  "SET_FAN_SPEED FAN=chamber_fan SPEED=0.4",
		"100": "SET_FAN_SPEED FAN=chamber_fan SPEED=1",
		"33":  "SET_FAN_SPEED FAN=chamber_fan SPEED=0.33",
	} {
		got, err := chamberFanScript("fan_generic chamber_fan", map[string]string{"chamber.fan_pwm_target": percent})
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestTheChamberFanScriptRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()

	for _, args := range []map[string]string{
		nil, {"chamber.fan_pwm_target": ""}, {"chamber.fan_pwm_target": "abc"}, {"chamber.fan_pwm_target": "NaN"},
		{"chamber.fan_pwm_target": "-1"}, {"chamber.fan_pwm_target": "101"}, {"chamber.fan_pwm_target": "5;M112"},
	} {
		_, err := chamberFanScript("fan_generic chamber_fan", args)
		require.Errorf(t, err, "%v", args)
	}

	_, err := chamberFanScript("", map[string]string{"chamber.fan_pwm_target": "50"})
	require.Error(t, err, "no fan was found")
}

func TestTheChamberFanSpeedIsAPercentage(t *testing.T) {
	t.Parallel()

	g := &gauges{roles: roles{chamberFan: "fan_generic chamber_fan"}}
	expect(t, run(t, g, `{"fan_generic chamber_fan": {"speed": 0.4}}`), map[string]string{chamberFanPath: "40"})
	expect(t, run(t, g, `{"fan_generic chamber_fan": {"speed": 0}}`), map[string]string{chamberFanPath: "0"})
	expect(t, run(t, &gauges{roles: roles{}}, `{"fan_generic chamber_fan": {"speed": 0.4}}`), map[string]string{})
}
