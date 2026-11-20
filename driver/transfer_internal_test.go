package moonraker

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/stretchr/testify/require"
)

type earlySuccessTransport struct{}

func (earlySuccessTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
}

type unopenedUpload struct{}

func (unopenedUpload) Size() int64 { return 1000 }
func (unopenedUpload) Open(context.Context, int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(strings.Repeat("x", 1000))), nil
}

func TestEarlySuccessDoesNotAcceptAnOldFileOfTheSameSize(t *testing.T) {
	host, err := moonrakersim.Start(t.Context(), moonrakersim.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = host.Close() })
	d := NewDriver(time.Second)
	connected, err := d.Connect(t.Context(), connectorapi.Endpoint{Address: host.Address()}, d.Baseline())
	require.NoError(t, err)
	s := connected.(*Session)
	t.Cleanup(func() { _ = s.Close() })
	s.upload = &http.Client{Transport: earlySuccessTransport{}}

	err = s.Send(t.Context(), connectorapi.Upload{Path: "gcodes/benchy.gcode", Content: unopenedUpload{}})
	require.ErrorIs(t, err, connectorapi.ErrTransferFailed)
}
