package moonraker_test

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// bytesContent is a Content over a slice.
type bytesContent []byte

func (b bytesContent) Size() int64 { return int64(len(b)) }

func (b bytesContent) Open(_ context.Context, offset int64) (io.ReadCloser, error) {
	offset = min(offset, int64(len(b)))

	return readCloser{Reader: bytesReader(b[offset:])}, nil
}

func bytesReader(b []byte) io.Reader { return &sliceReader{rest: b} }

type sliceReader struct{ rest []byte }

func (r *sliceReader) Read(p []byte) (int, error) {
	if len(r.rest) == 0 {
		return 0, io.EOF
	}

	n := copy(p, r.rest)
	r.rest = r.rest[n:]

	return n, nil
}

type readCloser struct{ io.Reader }

func (readCloser) Close() error { return nil }

// generatedContent is that many zero bytes, produced without allocating per byte.
type generatedContent int64

func (g generatedContent) Size() int64 { return int64(g) }

func (g generatedContent) Open(_ context.Context, offset int64) (io.ReadCloser, error) {
	return readCloser{Reader: &zeroReader{left: max(int64(g)-offset, 0)}}, nil
}

type zeroReader struct{ left int64 }

func (z *zeroReader) Read(p []byte) (int, error) {
	if z.left == 0 {
		return 0, io.EOF
	}

	n := int(min(int64(len(p)), z.left))
	clear(p[:n])
	z.left -= int64(n)

	return n, nil
}

func transfersOf(t *testing.T, s connectorapi.Session) connectorapi.Transfers {
	t.Helper()

	transfers, ok := connectorapi.Uploads(s)
	require.True(t, ok, "the session does not take uploads")

	return transfers
}

// progressLog appends without a lock, so -race reports a concurrent callback.
type progressLog struct{ reports []connectorapi.Progress }

func (p *progressLog) add(report connectorapi.Progress) { p.reports = append(p.reports, report) }
