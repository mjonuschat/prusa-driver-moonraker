package moonraker

import (
	"errors"
	"testing"
)

// TestAnAddressIsReadTheWayMoonrakerServesIt covers the three spellings an
// operator writes. A bare host takes Moonraker's own port, a URL keeps the path
// a reverse proxy put the API on, and a scheme this dialect does not speak is
// refused rather than dialled.
func TestAnAddressIsReadTheWayMoonrakerServesIt(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		address string
		want    string
		refused bool
	}{
		{name: "a bare host takes the usual port", address: "voron.local", want: "ws://voron.local:7125/websocket"},
		{name: "a host and port keeps the port", address: "10.0.0.5:7130", want: "ws://10.0.0.5:7130/websocket"},
		{name: "http becomes ws", address: "http://10.0.0.5:7125", want: "ws://10.0.0.5:7125/websocket"},
		{name: "https becomes wss", address: "https://voron.local", want: "wss://voron.local/websocket"},
		{name: "ws is left alone", address: "ws://10.0.0.5:7125/websocket", want: "ws://10.0.0.5:7125/websocket"},
		{
			name:    "a proxy path is kept",
			address: "https://printers.example/voron/websocket",
			want:    "wss://printers.example/voron/websocket",
		},
		{name: "a root path takes the api path", address: "http://10.0.0.5:7125/", want: "ws://10.0.0.5:7125/websocket"},
		{name: "an empty address is refused", address: "   ", refused: true},
		{name: "a scheme this dialect does not speak is refused", address: "ftp://10.0.0.5", refused: true},
		{name: "a url with no host is refused", address: "http:///websocket", refused: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := websocketURL(testCase.address)

			if testCase.refused {
				if err == nil {
					t.Fatalf("websocketURL(%q) answered %q and the address is not one", testCase.address, got)
				}

				if !errors.Is(err, ErrInvalidConfig) {
					t.Errorf("websocketURL(%q) refused with %v, which is not ErrInvalidConfig", testCase.address, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("websocketURL(%q): %v", testCase.address, err)
			}

			if got != testCase.want {
				t.Errorf("websocketURL(%q) is %q, and Moonraker serves %q", testCase.address, got, testCase.want)
			}
		})
	}
}
