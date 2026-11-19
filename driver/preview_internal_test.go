package moonraker

import (
	"bytes"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPickThumbnailTakesTheLargestPngUnderTheCap(t *testing.T) {
	t.Parallel()

	got, ok := pickThumbnail([]thumbnail{
		{Width: 300, Height: 300, Size: 4000, RelativePath: ".thumbs/a-300x300.png"},
		{Width: 32, Height: 32, Size: 400, RelativePath: ".thumbs/a-32x32.PNG"},
		{Width: 600, Height: 600, Size: 600 * 1024, RelativePath: ".thumbs/a-600x600.png"},
		{Width: 900, Height: 900, Size: 5000, RelativePath: ".thumbs/a-900x900.jpg"},
	})
	require.True(t, ok)
	require.Equal(t, ".thumbs/a-300x300.png", got.RelativePath)
}

func TestPickThumbnailRefusesAnEmptyOrJpgOnlyList(t *testing.T) {
	t.Parallel()

	_, ok := pickThumbnail(nil)
	require.False(t, ok)

	_, ok = pickThumbnail([]thumbnail{{Width: 9, Height: 9, Size: 1, RelativePath: "a.jpg"}})
	require.False(t, ok)
}

func TestThumbnailURLEscapesEachSegment(t *testing.T) {
	t.Parallel()

	require.Equal(t, "http://h:1/server/files/gcodes/models/.thumbs/a%20b.png",
		thumbnailURL("http://h:1", "models/odd name #1.gcode", ".thumbs/a b.png"))
	require.Equal(t, "http://h:1/server/files/gcodes/.thumbs/x.png",
		thumbnailURL("http://h:1", "benchy.gcode", ".thumbs/x.png"))
}

func TestHTTPBaseDerivesFromTheWebsocketAddressWithoutCredentials(t *testing.T) {
	t.Parallel()

	got, err := httpBase("ws://user:pw@h:7125/websocket")
	require.NoError(t, err)
	require.Equal(t, "http://h:7125", got)

	got, err = httpBase("wss://user:pw@h/proxy/websocket")
	require.NoError(t, err)
	require.Equal(t, "https://h/proxy", got)
}

func TestFetcherRefusesARedirectAnOversizeBodyAndANon200(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/missing":
			http.NotFound(w, r)
		default:
			_, _ = w.Write([]byte(strings.Repeat("x", 10)))
		}
	}))
	t.Cleanup(server.Close)

	f := newFetcher(time.Second)

	body, err := f.get(t.Context(), server.URL+"/ok", 10)
	require.NoError(t, err)
	require.Len(t, body, 10)

	_, err = f.get(t.Context(), server.URL+"/ok", 9)
	require.Error(t, err)

	_, err = f.get(t.Context(), server.URL+"/redirect", 100)
	require.Error(t, err)

	_, err = f.get(t.Context(), server.URL+"/missing", 100)
	require.Error(t, err)
}

func TestAPreviewFetchFailureNeverLogsTheCredentials(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	base, err := httpBase("ws://user:secret@" + address + "/websocket")
	require.NoError(t, err)

	var logs bytes.Buffer

	session := &Session{
		timeout:  time.Second,
		httpBase: base,
		fetch:    newFetcher(time.Second),
		log:      slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}

	got := session.previewFrom(t.Context(), "a.gcode", &metadataReply{
		Thumbnails: []thumbnail{{Width: 1, Height: 1, Size: 1, RelativePath: "a.png"}},
	})
	require.Nil(t, got)
	require.NotEmpty(t, logs.String())
	require.NotContains(t, logs.String(), "secret")
}
