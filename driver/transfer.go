package moonraker

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	uploadEndpoint = "/server/files/upload"
	// progressStep is how many bytes the file grows by between two reports.
	progressStep = 1 << 20
)

var _ connectorapi.Transfers = (*Session)(nil)

// Send streams one file to the gcodes root and lists the destination folder
// afterwards.
//
// A goroutine writes the body into a pipe, so the file is never held in
// memory. Send does not return before that goroutine has, and the wait has no
// bound. The SDK keeps Content in use until Send returns, and a reader
// that honors neither ctx nor Close is a Content bug, not something to leak a
// goroutine over.
func (s *Session) Send(ctx context.Context, up connectorapi.Upload) error {
	if !s.caps.Has(connectorapi.VerbFileUpload) {
		return connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", connectorapi.VerbFileUpload.String()))
	}

	if !s.sending.CompareAndSwap(false, true) {
		return connectorapi.ErrTransferBusy
	}
	defer s.sending.Store(false)

	relative, err := gcodesPath(up.Path)
	if err != nil {
		return err
	}

	if relative == "" {
		return connectorapi.PathNotAllowed.
			New("the gcodes root is not a file").
			With(fault.StringParam("path", up.Path))
	}

	folder := path.Dir(relative)
	if folder == "." {
		folder = ""
	}

	// runCtx ends the request and the Open of the content when Send leaves, so
	// an answer that comes before the body is read does not strand either.
	runCtx, cancel := context.WithCancel(ctx)
	pipeReader, pipeWriter := io.Pipe()
	form := multipart.NewWriter(pipeWriter)
	content := &heldReader{}
	reports := &reporter{total: up.Content.Size(), call: up.Progress}
	written := make(chan struct{})

	stopWatch := context.AfterFunc(runCtx, content.close)

	go func() {
		defer close(written)

		pipeWriter.CloseWithError(writeForm(runCtx, form, up.Content, content, reports, folder, path.Base(relative)))
	}()

	defer func() {
		stopWatch()
		cancel()
		_ = pipeReader.Close()
		content.close()
		<-written
	}()

	target := s.httpBase + uploadEndpoint

	request, err := http.NewRequestWithContext(runCtx, http.MethodPost, target, pipeReader)
	if err != nil {
		return fmt.Errorf("%w: %w", connectorapi.ErrTransferFailed, redactCause(err, target, target))
	}

	request.Header.Set("Content-Type", form.FormDataContentType())

	reports.report(0)

	response, err := s.upload.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %w", connectorapi.ErrTransferFailed, redactCause(err, target, target))
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
	_ = response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("%w: the upload answered %d", connectorapi.ErrTransferFailed, response.StatusCode)
	}

	// The writer has finished once the machine has read the whole body, but
	// wait for it so the last report carries the final count.
	_ = pipeReader.Close()

	<-written

	reports.report(reports.sent())

	listed := gcodesRoot
	if folder != "" {
		listed += "/" + folder
	}

	_, refusal, listErr := s.directory(ctx, listed)
	if listErr != nil {
		return listErr
	}

	if refusal != nil {
		return fmt.Errorf("%w: %s: %s", ErrRefused, methodGetDirectory, refusal.Message)
	}

	return nil
}

// writeForm writes the multipart body: root, path when the folder is not the
// root, then the file, last.
func writeForm(
	ctx context.Context, form *multipart.Writer, source connectorapi.Content,
	held *heldReader, reports *reporter, folder, name string,
) error {
	if err := form.WriteField("root", gcodesRoot); err != nil {
		return err
	}

	if folder != "" {
		if err := form.WriteField("path", folder); err != nil {
			return err
		}
	}

	part, err := form.CreateFormFile("file", name)
	if err != nil {
		return err
	}

	reader, err := source.Open(ctx, 0)
	if err != nil {
		return err
	}

	if !held.set(reader) {
		return ctx.Err()
	}

	if _, err = io.Copy(part, &countingReader{reader: reader, reports: reports}); err != nil {
		return err
	}

	return form.Close()
}

// heldReader is the content reader, kept where the watcher and the exit path
// can close it to unblock a Read.
type heldReader struct {
	mu     sync.Mutex
	reader io.Closer
	closed bool
}

// set keeps the reader, or closes it at once and reports false when close has
// already run.
func (h *heldReader) set(reader io.ReadCloser) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		_ = reader.Close()

		return false
	}

	h.reader = reader

	return true
}

func (h *heldReader) close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.closed = true

	if h.reader != nil {
		_ = h.reader.Close()
		h.reader = nil
	}
}

// reporter turns byte counts into Progress calls. One mutex covers the opening
// report, the reads and the closing report, so the callback never overlaps itself.
type reporter struct {
	mu       sync.Mutex
	total    int64
	count    int64
	reported int64
	call     func(connectorapi.Progress)
}

func (r *reporter) report(sent int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.emit(sent)
}

func (r *reporter) emit(sent int64) {
	r.reported = sent

	if r.call != nil {
		r.call(connectorapi.Progress{Total: r.total, Sent: sent})
	}
}

func (r *reporter) sent() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.count
}

func (r *reporter) add(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.count += int64(n)
	if r.count-r.reported >= progressStep {
		r.emit(r.count)
	}
}

type countingReader struct {
	reader  io.Reader
	reports *reporter
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	if n > 0 {
		c.reports.add(n)
	}

	return n, err
}
