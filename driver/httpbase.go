package moonraker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpBase is the HTTP address of the Moonraker that the websocket target
// reaches. A proxy path before /websocket is kept and the userinfo is dropped,
// so a base never carries a credential into an error or a log line.
func httpBase(websocketTarget string) (string, error) {
	parsed, err := url.Parse(websocketTarget)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}

		return "", fmt.Errorf("%w: the websocket address is not readable: %w", ErrInvalidConfig, err)
	}

	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	default:
		return "", fmt.Errorf("%w: the websocket address names the scheme %q", ErrInvalidConfig, parsed.Scheme)
	}

	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimSuffix(strings.TrimSuffix(parsed.Path, websocketPath), "/")
	parsed.RawPath = ""

	return parsed.String(), nil
}

// fetcher reads small bodies over HTTP.
type fetcher struct {
	client *http.Client
}

func newFetcher(timeout time.Duration) *fetcher {
	return &fetcher{client: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// get returns the body of a 200 answer of at most limit bytes. A redirect, any
// other status and a longer body are errors.
func (f *fetcher) get(ctx context.Context, target string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, redactCause(err, target, target)
	}

	response, err := f.client.Do(request)
	if err != nil {
		return nil, redactCause(err, target, target)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("moonraker: GET %s answered %d", redactAddress(target), response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, redactCause(err, target, target)
	}

	if int64(len(body)) > limit {
		return nil, fmt.Errorf("moonraker: GET %s sent more than %d bytes", redactAddress(target), limit)
	}

	return body, nil
}
