package moonraker

import (
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func objectsIn(patches []connectorapi.StatePatch) (string, bool) {
	for _, patch := range patches {
		if patch.Path == connectorapi.JobObjectsPath {
			return string(patch.Value), true
		}
	}

	return "", false
}

func exclude(t *testing.T, held *reader, body string) (string, bool) {
	t.Helper()

	return objectsIn(held.patchesFrom(reading(`{"exclude_object": `+body+`}`, t), 0, time.Unix(0, 0)))
}

func TestObjectsArePublishedByIndexInKlippersOrder(t *testing.T) {
	t.Parallel()

	got, ok := exclude(t, &reader{}, `{
		"objects": [{"name": "A_1"}, {"name": "B_2", "center": [1, 2]}, {"name": "SKIRT"}],
		"excluded_objects": ["B_2"], "current_object": "A_1"
	}`)

	require.True(t, ok)
	assert.JSONEq(t, `[
		{"id":0,"name":"A_1","cancelled":false},
		{"id":1,"name":"B_2","cancelled":true},
		{"id":2,"name":"SKIRT","cancelled":false}
	]`, got)
}

func TestAPartialFrameKeepsTheOtherHalf(t *testing.T) {
	t.Parallel()

	held := &reader{}
	exclude(t, held, `{"objects": [{"name": "A"}, {"name": "B"}]}`)

	got, ok := exclude(t, held, `{"excluded_objects": ["A"]}`)
	require.True(t, ok)
	assert.JSONEq(t, `[{"id":0,"name":"A","cancelled":true},{"id":1,"name":"B","cancelled":false}]`, got)

	got, ok = exclude(t, held, `{"excluded_objects": []}`)
	require.True(t, ok)
	assert.JSONEq(t, `[{"id":0,"name":"A","cancelled":false},{"id":1,"name":"B","cancelled":false}]`, got)
}

func TestObjectsAreWrittenOnlyWhenTheyChange(t *testing.T) {
	t.Parallel()

	held := &reader{}
	_, first := exclude(t, held, `{"objects": [{"name": "A"}], "current_object": "A"}`)
	_, again := exclude(t, held, `{"objects": [{"name": "A"}]}`)
	_, moved := exclude(t, held, `{"current_object": null}`)

	assert.True(t, first)
	assert.False(t, again)
	assert.False(t, moved, "the current object is not part of what Connect shows")
}

func TestExcludedNamesAloneAreNotPublishedBeforeTheObjectsAreKnown(t *testing.T) {
	t.Parallel()

	_, ok := exclude(t, &reader{}, `{"excluded_objects": ["A"]}`)
	assert.False(t, ok)
}

func TestAnEmptyObjectListIsAnEmptyArray(t *testing.T) {
	t.Parallel()

	got, ok := exclude(t, &reader{}, `{"objects": [], "excluded_objects": []}`)
	require.True(t, ok)
	assert.Equal(t, "[]", got)
}

func TestAMachineWithNoExcludeObjectPublishesNoObjects(t *testing.T) {
	t.Parallel()

	held := &reader{}
	_, ok := objectsIn(held.patchesFrom(reading(`{"print_stats": {"state": "standby"}}`, t), 0, time.Unix(0, 0)))
	assert.False(t, ok)
}

func TestAnObjectIsFoundByItsIndex(t *testing.T) {
	t.Parallel()

	held := &reader{}
	exclude(t, held, `{"objects": [{"name": "A"}, {"name": "B"}]}`)

	name, count, found := held.objects.nameOf(1)
	assert.True(t, found)
	assert.Equal(t, "B", name)
	assert.Equal(t, 2, count)

	_, count, found = held.objects.nameOf(2)
	assert.False(t, found)
	assert.Equal(t, 2, count)
}

func TestTheObjectScriptsAreKlippersOwn(t *testing.T) {
	t.Parallel()

	script, err := objectScript("STEPPER_MOUNT_1", true)
	require.NoError(t, err)
	assert.Equal(t, "EXCLUDE_OBJECT NAME=STEPPER_MOUNT_1", script)

	script, err = objectScript("STEPPER_MOUNT_1", false)
	require.NoError(t, err)
	assert.Equal(t, "EXCLUDE_OBJECT RESET=1 NAME=STEPPER_MOUNT_1", script)
}

func TestAnObjectNameThatCouldBeASecondCommandIsRefused(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"A B", "A;M112", "A\nM112", ""} {
		_, err := objectScript(name, true)
		require.Errorf(t, err, "%q", name)
	}
}
