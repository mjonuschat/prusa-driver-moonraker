package moonraker_test

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mjonuschat/prusa-driver-moonraker/driver"
	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

var pngBody = []byte("\x89PNG not really")

func listFile(t *testing.T, opts moonrakersim.Options) (connectorapi.Listing, *moonrakersim.Host) {
	t.Helper()

	machine := startMachine(t, opts)
	got, err := filesOf(t, connect(t, machine)).List(t.Context(), "gcodes/benchy.gcode")
	require.NoError(t, err)

	return got, machine
}

func TestAFileListingCarriesThePreview(t *testing.T) {
	t.Parallel()

	got, _ := listFile(t, moonrakersim.Options{Thumbnails: []moonrakersim.Thumbnail{{
		Width: 300, Height: 300, Size: len(pngBody),
		RelativePath: ".thumbs/benchy-300x300.png", Body: pngBody,
	}}})
	require.Equal(t, pngBody, got.Directory.Preview)
	require.Equal(t, int64(1000), got.Directory.Size)
	require.Equal(t, int64(1700000000), got.Directory.Modified)
}

func TestAFolderListingCarriesNoPreview(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	got, err := filesOf(t, connect(t, machine)).List(t.Context(), "gcodes")
	require.NoError(t, err)

	for _, entry := range got.Entries {
		require.Nil(t, entry.Preview)
	}

	require.Empty(t, machine.Requests("server.files.metadata"))
}

func TestAnOversizeThumbnailGivesNoPreviewAndNoError(t *testing.T) {
	t.Parallel()

	got, _ := listFile(t, moonrakersim.Options{Thumbnails: []moonrakersim.Thumbnail{{
		Width: 600, Height: 600, Size: 600 * 1024, RelativePath: ".thumbs/benchy-600x600.png", Body: pngBody,
	}}})
	require.Nil(t, got.Directory.Preview)
}

func TestAFailedThumbnailFetchGivesNoPreviewAndNoError(t *testing.T) {
	t.Parallel()

	got, _ := listFile(t, moonrakersim.Options{Thumbnails: []moonrakersim.Thumbnail{{
		Width: 300, Height: 300, Size: 10, RelativePath: ".thumbs/benchy-300x300.png",
	}}})
	require.Nil(t, got.Directory.Preview)
}

func TestAFileWithNoThumbnailsGivesNoPreview(t *testing.T) {
	t.Parallel()

	got, _ := listFile(t, moonrakersim.Options{NoThumbnails: true})
	require.Nil(t, got.Directory.Preview)
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestAPreviewErrorNeverCarriesTheAddressCredentials(t *testing.T) {
	t.Parallel()

	machine := startMachine(t, moonrakersim.Options{})
	logs := &lockedBuffer{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	d, err := moonraker.New(connectorapi.Spec{Logger: log})
	require.NoError(t, err)

	outbound, ok := d.(connectorapi.Outbound)
	require.True(t, ok)

	// The websocket library refuses userinfo in a dial URL, so a credentialed
	// address ends in a connect error. Neither it nor the log may carry the
	// password. The preview fetch itself is covered in the internal test.
	session, err := outbound.Connect(t.Context(),
		connectorapi.Endpoint{Address: "http://user:secret@" + machine.Address()}, outbound.Baseline())
	if session != nil {
		_ = session.Close()
	}

	require.Error(t, err)
	walk(t, err)
	require.NotContains(t, logs.String(), "secret")
}
