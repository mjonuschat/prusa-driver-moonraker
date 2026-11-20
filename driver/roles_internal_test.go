package moonraker

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

var (
	commonObjects = []string{"fan", "heater_fan hotend_fan", "fan_generic bed_fans", "temperature_sensor Chamber", "heater_bed", "extruder"}
	hankObjects   = commonObjects
	hankHeaters   = []string{"heater_bed", "extruder"}
	ukuObjects    = []string{
		"heater_bed", "heater_chamber", "heater_generic bed_fans", "extruder", "fan", "heater_fan hotend_fan",
		"filament_switch_sensor runout_sensor", "temperature_sensor Chamber",
	}
	ukuHeaters  = []string{"heater_bed", "heater_chamber", "heater_generic bed_fans", "extruder"}
	leleObjects = []string{
		"heater_bed", "heater_chamber", "heater_generic bed_fans", "extruder", "fan",
		"filament_switch_sensor runout_sensor",
	}
	busterObjects = []string{"heater_bed", "extruder", "fan", "heater_fan hotend_fan", "temperature_sensor Chamber"}
	busterHeaters = []string{"heater_bed", "extruder"}
)

func TestHankResolvesASensorChamberAndABedFan(t *testing.T) {
	t.Parallel()
	r := resolveRoles(hankObjects, hankHeaters, roleOptions{})
	require.Equal(t, chamberRole{object: "temperature_sensor Chamber"}, r.chamber)
	require.Equal(t, "fan_generic bed_fans", r.bedFan)
	require.Equal(t, "fan", r.partFan)
	require.Equal(t, "heater_fan hotend_fan", r.heatbreakFan)
	require.Empty(t, r.filament)
}

func TestUkuAndLeleResolveTheChamberHeaterAndDemoteBedFans(t *testing.T) {
	t.Parallel()
	r := resolveRoles(ukuObjects, ukuHeaters, roleOptions{})
	require.Equal(t, chamberRole{object: "heater_chamber", heater: true}, r.chamber)
	require.Equal(t, "heater_generic bed_fans", r.bedFan)
	require.Equal(t, "filament_switch_sensor runout_sensor", r.filament)
	require.Equal(t, "heater_fan hotend_fan", r.heatbreakFan)

	l := resolveRoles(leleObjects, ukuHeaters, roleOptions{})
	require.Equal(t, chamberRole{object: "heater_chamber", heater: true}, l.chamber)
	require.Equal(t, "heater_generic bed_fans", l.bedFan)
	require.Equal(t, "filament_switch_sensor runout_sensor", l.filament)
	require.Empty(t, l.heatbreakFan)
}

func TestBusterResolvesASensorChamberAndNoBedFan(t *testing.T) {
	t.Parallel()
	r := resolveRoles(busterObjects, busterHeaters, roleOptions{})
	require.Equal(t, chamberRole{object: "temperature_sensor Chamber"}, r.chamber)
	require.Empty(t, r.bedFan)
}

func TestABedFansHeaterAloneIsTheChamber(t *testing.T) {
	t.Parallel()
	r := resolveRoles([]string{"heater_generic bed_fans"}, []string{"heater_generic bed_fans"}, roleOptions{})
	require.Equal(t, chamberRole{object: "heater_generic bed_fans", heater: true}, r.chamber)
	require.Empty(t, r.bedFan)
}

func TestNamesMatchInAnyCase(t *testing.T) {
	t.Parallel()
	listed := []string{"HEATER_GENERIC Bed_Fans", "Heater_Chamber"}
	r := resolveRoles(listed, listed, roleOptions{})
	require.Equal(t, chamberRole{object: "Heater_Chamber", heater: true}, r.chamber)
	require.Equal(t, "HEATER_GENERIC Bed_Fans", r.bedFan)
}

func TestAnOverrideBeatsTheDefault(t *testing.T) {
	t.Parallel()
	r := resolveRoles(ukuObjects, ukuHeaters, roleOptions{chamber: "temperature_sensor Chamber", partFan: "heater_fan hotend_fan"})
	require.Equal(t, chamberRole{object: "temperature_sensor Chamber"}, r.chamber)
	require.Equal(t, "heater_fan hotend_fan", r.partFan)
}

func TestNoneSwitchesARoleOff(t *testing.T) {
	t.Parallel()
	r := resolveRoles(ukuObjects, ukuHeaters, roleOptions{chamber: "none", partFan: "NONE", filament: "none"})
	require.Equal(t, chamberRole{}, r.chamber)
	require.Empty(t, r.partFan)
	require.Empty(t, r.filament)
	require.Empty(t, r.unresolved)
}

func TestAnUnlistedOverrideIsNoObjectAndIsReportedWithItsOption(t *testing.T) {
	t.Parallel()
	r := resolveRoles(hankObjects, hankHeaters, roleOptions{chamber: "nope"})
	require.Empty(t, r.chamber.object)
	require.Equal(t, []unresolved{{"chamber_object", "nope"}}, r.unresolved)
}

func TestTheSameUnlistedValueInTwoOptionsIsTwoEntries(t *testing.T) {
	t.Parallel()
	r := resolveRoles(hankObjects, hankHeaters, roleOptions{extraSensors: []string{"ghost"}, extraFans: []string{"ghost"}})
	require.Equal(t, []unresolved{{"extra_sensors", "ghost"}, {"extra_fans", "ghost"}}, r.unresolved)
}

func TestAListedNonHeaterOverrideIsAReadingOnlyChamber(t *testing.T) {
	t.Parallel()
	r := resolveRoles(hankObjects, hankHeaters, roleOptions{chamber: "fan"})
	require.Equal(t, chamberRole{object: "fan"}, r.chamber)
}

func TestAChamberOverrideThatIsAHeaterKeepsTheVerb(t *testing.T) {
	t.Parallel()
	r := resolveRoles(ukuObjects, ukuHeaters, roleOptions{chamber: "heater_generic bed_fans"})
	require.Equal(t, chamberRole{object: "heater_generic bed_fans", heater: true}, r.chamber)
	require.Empty(t, r.bedFan)

	r = resolveRoles(ukuObjects, ukuHeaters, roleOptions{chamber: "heater_bed"})
	require.True(t, r.chamber.heater)
	require.Equal(t, "heater_generic bed_fans", r.bedFan)
}

func TestAnOverrideMayOmitTheTypePrefixWhenUnambiguous(t *testing.T) {
	t.Parallel()
	r := resolveRoles(ukuObjects, ukuHeaters, roleOptions{filament: "runout_sensor"})
	require.Equal(t, "filament_switch_sensor runout_sensor", r.filament)

	listed := []string{"fan_generic x", "temperature_sensor x"}
	r = resolveRoles(listed, nil, roleOptions{partFan: "x"})
	require.Empty(t, r.partFan)
	require.Equal(t, []unresolved{{"part_fan_object", "x"}}, r.unresolved)
}

func TestTheExtraListsAreTrimmedAndDeduplicated(t *testing.T) {
	t.Parallel()
	o := roleOptionsOf(map[string]string{"extra_sensors": " a , ,a,b ", "chamber_object": "x"})
	require.Equal(t, []string{"a", "b"}, o.extraSensors)
	require.Equal(t, "x", o.chamber)
}

func TestHeaterNameIsTheNameAfterTheType(t *testing.T) {
	t.Parallel()
	require.Equal(t, "bed_fans", heaterName("heater_generic bed_fans"))
	require.Equal(t, "heater_chamber", heaterName("heater_chamber"))
}

func TestObjectsListsEachRoleObjectOnce(t *testing.T) {
	t.Parallel()
	r := resolveRoles(ukuObjects, ukuHeaters, roleOptions{extraSensors: []string{"heater_bed", "extruder"}, extraFans: []string{"fan"}})
	require.Equal(t, []string{
		"heater_chamber", "heater_generic bed_fans", "fan", "heater_fan hotend_fan",
		"filament_switch_sensor runout_sensor", "heater_bed", "extruder",
	}, r.objects())
}

func roleMachine(t *testing.T) *moonrakersim.Host {
	t.Helper()

	host, err := moonrakersim.Start(t.Context(), moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), "temperature_sensor Chamber", "fan", "heater_fan hotend_fan", "heaters"),
		Status: func() map[string]map[string]any {
			status := moonrakersim.DefaultStatus()
			status["temperature_sensor Chamber"] = map[string]any{"temperature": 26.4}
			status["fan"] = map[string]any{"speed": 0.5, "rpm": nil}
			status["heater_fan hotend_fan"] = map[string]any{"speed": 0.0}
			status["heaters"] = map[string]any{"available_heaters": []string{"extruder", "heater_bed"}}

			return status
		}(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = host.Close() })

	return host
}

func TestHandshakeAsksForTheRoleObjects(t *testing.T) {
	t.Parallel()

	host := roleMachine(t)

	conn, _, err := websocket.DefaultDialer.DialContext(t.Context(), "ws://"+host.Address()+"/websocket", nil)
	require.NoError(t, err)

	session := newSession(conn, 5*time.Second, liveness{}.orShipping(), slog.New(slog.DiscardHandler))
	t.Cleanup(session.hangUp)

	_, err = session.handshake(t.Context(), connectorapi.Set{})
	require.NoError(t, err)
	require.Contains(t, session.asked, "fan")
	require.Contains(t, session.asked, "temperature_sensor Chamber")
}

type captured struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *captured) Enabled(context.Context, slog.Level) bool { return true }
func (c *captured) WithAttrs([]slog.Attr) slog.Handler       { return c }
func (c *captured) WithGroup(string) slog.Handler            { return c }
func (c *captured) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r)

	return nil
}

func TestAnUnlistedOverrideIsLoggedWithItsOption(t *testing.T) {
	t.Parallel()

	host := roleMachine(t)
	handler := &captured{}

	driver := newDriver(5*time.Second, liveness{})
	driver.log = slog.New(handler)

	session, err := driver.Connect(t.Context(),
		connectorapi.Endpoint{Address: host.Address(), Options: map[string]string{"chamber_object": "nope"}},
		driver.Baseline())
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	handler.mu.Lock()
	defer handler.mu.Unlock()

	var warnings []map[string]string

	for _, record := range handler.records {
		if record.Level != slog.LevelWarn {
			continue
		}

		attrs := map[string]string{}
		record.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value.String(); return true })
		warnings = append(warnings, attrs)
	}

	require.Len(t, warnings, 1)
	require.Equal(t, "chamber_object", warnings[0]["option"])
	require.Equal(t, "nope", warnings[0]["value"])
}
