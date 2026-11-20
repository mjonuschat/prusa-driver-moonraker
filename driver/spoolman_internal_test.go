package moonraker

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func patchValues(t *testing.T, patches []connectorapi.StatePatch) []string {
	t.Helper()
	require.Len(t, patches, len(spoolPaths))
	out := make([]string, len(patches))
	for i, p := range patches {
		require.Equal(t, spoolPaths[i], p.Path)
		out[i] = string(p.Value)
	}
	return out
}

func decode(t *testing.T, body string) spoolReply {
	t.Helper()
	spool, err := decodeSpool(json.RawMessage(body))
	require.NoError(t, err)
	return spool
}

const leleSpool = `{"filament":{"material":"ABS","color_hex":"0020C2"},"initial_weight":1000,"used_weight":313.13}`

func TestASpoolBecomesFivePatches(t *testing.T) {
	t.Parallel()
	got := patchValues(t, spoolPatches(decode(t, leleSpool)))
	require.Equal(t, []string{`"ABS"`, `"ABS"`, `"#0020C2"`, `1000`, `313.13`}, got)
}

func TestAMissingOrNullFieldRemovesItsPath(t *testing.T) {
	t.Parallel()
	full := []string{`"ABS"`, `"ABS"`, `"#0020C2"`, `1000`, `313.13`}
	cases := map[string]struct {
		body string
		gone []int
	}{
		"no colour":      {`{"filament":{"material":"ABS"},"initial_weight":1000,"used_weight":313.13}`, []int{2}},
		"null colour":    {`{"filament":{"material":"ABS","color_hex":null},"initial_weight":1000,"used_weight":313.13}`, []int{2}},
		"null initial":   {`{"filament":{"material":"ABS","color_hex":"0020C2"},"initial_weight":null,"used_weight":313.13}`, []int{3}},
		"absent used":    {`{"filament":{"material":"ABS","color_hex":"0020C2"},"initial_weight":1000}`, []int{4}},
		"empty material": {`{"filament":{"material":"","color_hex":"0020C2"},"initial_weight":1000,"used_weight":313.13}`, []int{0, 1}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := append([]string(nil), full...)
			for _, i := range c.gone {
				want[i] = `null`
			}
			require.Equal(t, want, patchValues(t, spoolPatches(decode(t, c.body))))
		})
	}
}

func TestAColourThatIsNotHexRemovesTheColourPath(t *testing.T) {
	t.Parallel()
	for _, hex := range []string{`"xyz"`, `"0020C"`, `"#0020C2"`} {
		body := `{"filament":{"material":"ABS","color_hex":` + hex + `}}`
		require.Equal(t, `null`, patchValues(t, spoolPatches(decode(t, body)))[2], hex)
	}
	multi := `{"filament":{"material":"ABS","color_hex":"0020C2","multi_color_hexes":"0020C2,FFFFFF"}}`
	require.Equal(t, `null`, patchValues(t, spoolPatches(decode(t, multi)))[2])

	eight := `{"filament":{"material":"ABS","color_hex":"0020C2FF"}}`
	require.Equal(t, `"#0020C2FF"`, patchValues(t, spoolPatches(decode(t, eight)))[2])
}

func TestAnAlienWeightIsNotPublished(t *testing.T) {
	t.Parallel()
	for _, w := range []string{`"abc"`, `-1`} {
		body := `{"filament":{"material":"ABS"},"used_weight":` + w + `}`
		require.Equal(t, `null`, patchValues(t, spoolPatches(decode(t, body)))[4], w)
	}
}

func TestClearRemovesEveryPath(t *testing.T) {
	t.Parallel()
	got := patchValues(t, spoolClear())
	for _, v := range got {
		require.Equal(t, `null`, v)
	}
}

func TestAnUnreadableAnswerIsAnError(t *testing.T) {
	t.Parallel()
	_, err := decodeSpool(json.RawMessage(`[1,2]`))
	require.Error(t, err)
}

func TestAMethodNotFoundAnswerDisablesTheFeature(t *testing.T) {
	t.Parallel()

	require.True(t, isMethodNotFound(&rpcError{Code: -32601}))
	require.False(t, isMethodNotFound(&rpcError{Code: 400}))
	require.False(t, isMethodNotFound(nil))
}

func TestPrintEndedIsAPrintLeavingTheBed(t *testing.T) {
	t.Parallel()

	words := []string{"IDLE", "PRINTING", "PAUSED", "FINISHED", "STOPPED", "ERROR"}
	ends := map[[2]string]bool{}

	for _, from := range []string{"PRINTING", "PAUSED"} {
		for _, to := range []string{"FINISHED", "STOPPED", "ERROR"} {
			ends[[2]string{from, to}] = true
		}
	}

	for _, from := range words {
		for _, to := range words {
			require.Equal(t, ends[[2]string{from, to}], printEnded(from, to), "%s to %s", from, to)
		}
	}
}

func TestSyncRateReadsANumberOrText(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]time.Duration{"7": 7 * time.Second, `"3"`: 3 * time.Second, "1.5": 1500 * time.Millisecond} {
		got, ok := seconds(json.RawMessage(raw))
		require.True(t, ok, raw)
		require.Equal(t, want, got, raw)
	}

	for _, raw := range []string{"", "null", "0", "-1", `"x"`, "[1]"} {
		_, ok := seconds(json.RawMessage(raw))
		require.False(t, ok, raw)
	}
}
