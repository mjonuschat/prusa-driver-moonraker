package moonraker_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	moonraker "github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func objectMachine(t *testing.T) (*moonrakersim.Host, connectorapi.Session) {
	t.Helper()

	status := moonrakersim.DefaultStatus()
	status["exclude_object"] = map[string]any{
		"objects":          []any{map[string]any{"name": "PART_A"}, map[string]any{"name": "PART_B"}},
		"excluded_objects": []any{},
		"current_object":   nil,
	}

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), "exclude_object"), Status: status,
	})

	session := connect(t, machine)

	machine.Push(map[string]map[string]any{"print_stats": {"state": "printing", "filename": "models/part.gcode"}})
	collect(t, session, map[string]string{"/state": `"PRINTING"`})

	return machine, session
}

func TestObjectVerbsAreOfferedOnlyWithExcludeObject(t *testing.T) {
	t.Parallel()

	_, session := objectMachine(t)
	caps, _ := session.Capabilities()

	for _, key := range []connectorapi.Capability{
		moonraker.VerbCancelObject, moonraker.ArgCancelObject,
		moonraker.VerbUncancelObject, moonraker.ArgUncancelObject,
	} {
		require.Contains(t, caps, key)
	}

	assert.Equal(t, connectorapi.KindInteger, caps[moonraker.ArgCancelObject].Kind)
	assert.True(t, caps[moonraker.ArgCancelObject].Required)
	assert.Equal(t, 65535.0, limitOf(t, caps, moonraker.ArgCancelObject))

	plain := startMachine(t, moonrakersim.Options{
		Objects: moonrakersim.DefaultObjects(), Status: moonrakersim.DefaultStatus(),
	})
	plainCaps, _ := connect(t, plain).Capabilities()
	require.NotContains(t, plainCaps, moonraker.VerbCancelObject)
	require.NotContains(t, plainCaps, moonraker.VerbUncancelObject)
}

func TestTheObjectsOfThePrintAreReportedToConnect(t *testing.T) {
	t.Parallel()

	status := moonrakersim.DefaultStatus()
	status["exclude_object"] = map[string]any{
		"objects": []any{map[string]any{"name": "PART_A"}, map[string]any{"name": "PART_B"}},
		// Only PART_B is cancelled, and the list is reported in full.
		"excluded_objects": []any{"PART_B"},
		"current_object":   nil,
	}

	machine := startMachine(t, moonrakersim.Options{
		Objects: append(moonrakersim.DefaultObjects(), "exclude_object"), Status: status,
	})

	collect(t, connect(t, machine), map[string]string{
		"/job/objects": `[{"id":0,"name":"PART_A","cancelled":false},{"id":1,"name":"PART_B","cancelled":true}]`,
	})
}

func TestCancellingAnObjectExcludesItByName(t *testing.T) {
	t.Parallel()

	machine, session := objectMachine(t)

	result, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbCancelObject, Args: map[string]string{"id": "1"},
	})
	require.NoError(t, err)
	assert.True(t, result.OK)

	result, err = session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbUncancelObject, Args: map[string]string{"id": "0"},
	})
	require.NoError(t, err)
	assert.True(t, result.OK)

	assert.Equal(t, []string{"EXCLUDE_OBJECT NAME=PART_B", "EXCLUDE_OBJECT RESET=1 NAME=PART_A"}, machine.Scripts())
}

func TestAnObjectThatIsNotThereIsRefusedBeforeItReachesTheMachine(t *testing.T) {
	t.Parallel()

	machine, session := objectMachine(t)

	_, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbCancelObject, Args: map[string]string{"id": "2"},
	})
	require.Error(t, err)
	require.True(t, fault.HasCode(err, connectorapi.ArgumentOutOfRange.Code()))
	assert.Empty(t, machine.Scripts())
}

func TestKlippersRefusalOfAnObjectIsReturnedAsItIs(t *testing.T) {
	t.Parallel()

	machine, session := objectMachine(t)
	machine.RefuseScript("EXCLUDE_OBJECT", "Unable to exclude")

	result, err := session.Do(t.Context(), connectorapi.Command{
		Verb: moonraker.VerbCancelObject, Args: map[string]string{"id": "0"},
	})
	require.NoError(t, err)
	assert.False(t, result.OK)
	assert.Equal(t, "Unable to exclude", result.MachineReason)
}

func TestObjectVerbsAreAcceptedOnlyWhilePrintingOrPaused(t *testing.T) {
	t.Parallel()

	set := declaredSet(t)

	for _, verb := range []connectorapi.Capability{moonraker.VerbCancelObject, moonraker.VerbUncancelObject} {
		for state, want := range map[connectorapi.StateName]bool{
			"IDLE": false, "PRINTING": true, "PAUSED": true, "FINISHED": false, "STOPPED": false, "ERROR": false,
		} {
			require.Equalf(t, want, set.Accepted(state, verb), "%s in %s", verb, state)
		}
	}
}
