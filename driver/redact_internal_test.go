package moonraker

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactAddressRemovesUserinfo(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"http://user:secret@host:7125/x": "http://host:7125/x",
		"user:secret@host":               "host",
		"ws://host":                      "ws://host",
		"http://u:p@a@host":              "http://host",
		"http://user:secret@%zz":         "http://%zz",
	} {
		require.Equal(t, want, redactAddress(in), in)
	}
}

func TestRedactCauseRemovesTheAddressFromTheDialError(t *testing.T) {
	t.Parallel()

	cases := []struct{ cause, address, want string }{
		{
			`parse "ws://[u:p@a@127.0.0.1:1]:7125/websocket": net/url: invalid userinfo`,
			"u:p@a@127.0.0.1:1", "invalid userinfo",
		},
		{
			`parse "ws://user:secret@%zz/websocket": invalid URL escape "%zz"`,
			"user:secret@%zz", "invalid URL escape",
		},
	}

	for _, tc := range cases {
		target, err := websocketURL(tc.address)
		if err != nil {
			target = "ws://" + tc.address + websocketPath
		}

		result := redactCause(errors.New(tc.cause), tc.address, target)

		require.NotContains(t, result.Error(), "secret")
		require.NotContains(t, result.Error(), "u:p")
		require.Contains(t, result.Error(), tc.want)
		require.NoError(t, errors.Unwrap(result))
	}
}
