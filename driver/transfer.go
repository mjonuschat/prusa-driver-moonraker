package moonraker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
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
		busy := connectorapi.TransferBusy.
			New("a transfer is already running").
			With(fault.StringParam("path", up.Path))

		return fmt.Errorf("%w: %w", connectorapi.ErrTransferBusy, busy)
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
	var writeErr error

	stopWatch := context.AfterFunc(runCtx, content.close)

	go func() {
		defer close(written)

		writeErr = writeForm(runCtx, form, up.Content, content, reports, folder, path.Base(relative))
		pipeWriter.CloseWithError(writeErr)
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
		return transferFailed(up.Path, redactCause(err, target, target))
	}

	request.Header.Set("Content-Type", form.FormDataContentType())
	setAPIKey(request, s.apiKey)

	reports.report(0)

	response, err := s.upload.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return transferFailed(up.Path, redactCause(err, target, target))
	}

	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	_ = response.Body.Close()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return transferRefused(up.Path, response.StatusCode, body)
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return transferFailed(up.Path, fmt.Errorf("the upload answered %d", response.StatusCode))
	}

	// The writer has finished once the machine has read the whole body, but
	// wait for it so the last report carries the final count.
	cancel()
	_ = pipeReader.Close()

	<-written
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if writeErr != nil {
		return transferFailed(up.Path, redactCause(writeErr, target, target))
	}

	reports.report(reports.sent())

	listed := gcodesRoot
	if folder != "" {
		listed += "/" + folder
	}

	reply, refusal, listErr := s.directory(ctx, listed)
	if listErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return listErr
	}

	if refusal != nil {
		return fmt.Errorf("%w: %s: %s", ErrRefused, methodGetDirectory, refusal.Message)
	}

	return checkStored(up.Path, path.Base(relative), up.Content.Size(), reply)
}

func checkStored(asked, name string, size int64, reply *directoryReply) error {
	found := "absent"

	for _, dir := range reply.Dirs {
		if dir.Dirname == name {
			found = "folder"
		}
	}

	for _, file := range reply.Files {
		if file.Filename == name {
			if file.Size == size {
				return nil
			}

			found = strconv.FormatInt(file.Size, 10)
		}
	}

	return connectorapi.TransferNotOnStorage.
		New("the printer finished the upload and the file is not on its storage as sent").
		With(
			fault.StringParam("path", asked),
			fault.IntParam("expected_size", size),
			fault.StringParam("found", found),
		)
}

func transferFailed(asked string, cause error) error {
	failed := connectorapi.TransferFailed.
		New("the upload did not finish").
		With(fault.StringParam("path", asked), fault.StringParam("machine_reason", cause.Error()))

	return fmt.Errorf("%w: %w", connectorapi.ErrTransferFailed, failed)
}

// transferRefused takes the reason from Moonraker's {"error":{"message":...}}
// body, or from the status text when the body has none.
func transferRefused(asked string, status int, body []byte) error {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	var reason string

	if json.Unmarshal(body, &parsed) == nil {
		reason = parsed.Error.Message
	}

	if reason == "" {
		reason = http.StatusText(status)
	}

	refused := connectorapi.TransferRefused.
		New("the printer refused the upload").
		With(
			fault.StringParam("path", asked),
			fault.StringParam("machine_reason", reason),
			fault.StringParam("reason", reason),
		)

	return fmt.Errorf("%w: %w", connectorapi.ErrTransferRefused, refused)
}

// writeForm writes the multipart body in this order: root, path when the
// folder is not the root, then the file.
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
