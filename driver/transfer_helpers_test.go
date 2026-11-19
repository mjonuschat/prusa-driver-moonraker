package moonraker_test

import (
	"context"
	"errors"
	"io"
	"sync"
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

type failingReader struct {
	left int
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.left == 0 {
		return 0, f.err
	}

	n := min(len(p), f.left)
	clear(p[:n])
	f.left -= n

	return n, nil
}

type failingContent struct{ openFails bool }

func (failingContent) Size() int64 { return 1000 }

func (f failingContent) Open(context.Context, int64) (io.ReadCloser, error) {
	if f.openFails {
		return nil, errors.New("cannot open")
	}

	return readCloser{Reader: &failingReader{left: 100, err: errors.New("disk gone")}}, nil
}

type gatedContent struct {
	released chan struct{}
	closed   chan struct{}
	once     sync.Once
	reading  chan struct{}
}

func newGatedContent() *gatedContent {
	return &gatedContent{
		released: make(chan struct{}), closed: make(chan struct{}), reading: make(chan struct{}),
	}
}

func (g *gatedContent) Size() int64 { return 200 }

func (g *gatedContent) Open(context.Context, int64) (io.ReadCloser, error) {
	return &gatedReader{g: g}, nil
}

type gatedReader struct {
	g    *gatedContent
	sent int
}

func (r *gatedReader) Read(p []byte) (int, error) {
	if r.sent < 100 {
		n := min(len(p), 100-r.sent)
		clear(p[:n])
		r.sent += n

		return n, nil
	}

	r.g.once.Do(func() { close(r.g.reading) })

	select {
	case <-r.g.released:
	case <-r.g.closed:
		return 0, io.ErrClosedPipe
	}

	if r.sent >= 200 {
		return 0, io.EOF
	}

	n := min(len(p), 200-r.sent)
	clear(p[:n])
	r.sent += n

	return n, nil
}

func (r *gatedReader) Close() error {
	select {
	case <-r.g.closed:
	default:
		close(r.g.closed)
	}

	return nil
}

type blockedOpenContent struct{}

func (blockedOpenContent) Size() int64 { return 10 }

func (blockedOpenContent) Open(ctx context.Context, _ int64) (io.ReadCloser, error) {
	<-ctx.Done()

	return nil, ctx.Err()
}
