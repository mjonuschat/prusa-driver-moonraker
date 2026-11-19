package moonrakersim_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func uploadBody(t *testing.T, root, dir, name string, size int) (io.Reader, string) {
	t.Helper()

	var buf bytes.Buffer

	form := multipart.NewWriter(&buf)
	_ = form.WriteField("root", root)
	_ = form.WriteField("path", dir)

	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = part.Write(bytes.Repeat([]byte("g"), size))

	if err := form.Close(); err != nil {
		t.Fatal(err)
	}

	return &buf, form.FormDataContentType()
}

func upload(t *testing.T, r *rig, root, dir, name string, size int) (int, string, error) {
	t.Helper()

	body, kind := uploadBody(t, root, dir, name, size)

	resp, err := http.Post("http://"+r.host.Address()+"/server/files/upload", kind, body)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	text, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, string(text), nil
}

func listedSize(t *testing.T, r *rig, dir, name string) (int64, bool) {
	t.Helper()

	var got listing

	r.ok("server.files.get_directory", map[string]any{"path": dir}, &got)

	for _, f := range got.Files {
		if f.Filename == name {
			return f.Size, true
		}
	}

	return 0, false
}

func TestAnUploadIsRecordedAndListed(t *testing.T) {
	r := start(t, moonrakersim.Options{})

	status, _, err := upload(t, r, "gcodes", "models", "new.gcode", 10)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("status %d, err %v", status, err)
	}

	want := []moonrakersim.Upload{{Root: "gcodes", Path: "models", Name: "new.gcode", Size: 10}}
	if got := r.host.Uploads(); !reflect.DeepEqual(got, want) {
		t.Fatalf("uploads = %+v", got)
	}

	if size, ok := listedSize(t, r, "gcodes/models", "new.gcode"); !ok || size != 10 {
		t.Fatalf("listed %d, %v", size, ok)
	}
}

func TestAnUploadReplacesAFileOfTheSameName(t *testing.T) {
	r := start(t, moonrakersim.Options{})

	if status, _, err := upload(t, r, "gcodes", "", "benchy.gcode", 5); err != nil || status != http.StatusCreated {
		t.Fatalf("status %d, err %v", status, err)
	}

	if size, _ := listedSize(t, r, "gcodes", "benchy.gcode"); size != 5 {
		t.Fatalf("size = %d", size)
	}
}

func TestAnUploadToAReadOnlyRootIs403(t *testing.T) {
	r := start(t, moonrakersim.Options{ReadOnlyGcodes: true})

	status, body, err := upload(t, r, "gcodes", "", "a.gcode", 3)
	if err != nil || status != http.StatusForbidden || !strings.Contains(body, `"error"`) {
		t.Fatalf("status %d, body %q, err %v", status, body, err)
	}
}

func TestAnUploadRefusalIsAnsweredVerbatim(t *testing.T) {
	const refusal = `{"error":{"code":403,"message":"File is loaded"}}`

	r := start(t, moonrakersim.Options{UploadRefusal: &moonrakersim.UploadRefusal{Status: 403, Body: refusal}})

	status, body, err := upload(t, r, "gcodes", "", "a.gcode", 3)
	if err != nil || status != 403 || body != refusal {
		t.Fatalf("status %d, body %q, err %v", status, body, err)
	}

	if got := r.host.Uploads(); len(got) != 0 {
		t.Fatalf("uploads = %+v", got)
	}
}

func TestARefusalIsSentBeforeTheBodyIsRead(t *testing.T) {
	r := start(t, moonrakersim.Options{UploadRefusal: &moonrakersim.UploadRefusal{Status: 403, Body: `{}`}})

	pipe, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })

	req, err := http.NewRequest(http.MethodPost, "http://"+r.host.Address()+"/server/files/upload", pipe)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")

	done := make(chan int, 1)

	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- -1

			return
		}

		_ = resp.Body.Close()
		done <- resp.StatusCode
	}()

	select {
	case status := <-done:
		if status != 403 {
			t.Fatalf("status = %d", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no answer before the body was read")
	}
}

func TestClearingTheRefusalLetsAnUploadThrough(t *testing.T) {
	r := start(t, moonrakersim.Options{UploadRefusal: &moonrakersim.UploadRefusal{Status: 403, Body: `{}`}})

	if status, _, _ := upload(t, r, "gcodes", "", "a.gcode", 10); status != 403 {
		t.Fatalf("status = %d", status)
	}

	r.host.ClearUploadRefusal()

	if status, _, err := upload(t, r, "gcodes", "", "a.gcode", 10); err != nil || status != http.StatusCreated {
		t.Fatalf("status %d, err %v", status, err)
	}
}

func TestADroppedUploadStoresNothing(t *testing.T) {
	r := start(t, moonrakersim.Options{UploadDropAfter: 4})

	if _, _, err := upload(t, r, "gcodes", "", "gone.gcode", 10); err == nil {
		t.Fatal("expected a connection error")
	}

	if _, ok := listedSize(t, r, "gcodes", "gone.gcode"); ok {
		t.Fatal("dropped upload was stored")
	}

	if got := r.host.Uploads(); len(got) != 0 {
		t.Fatalf("uploads = %+v", got)
	}
}

func TestAnUploadCanStoreAnotherSize(t *testing.T) {
	r := start(t, moonrakersim.Options{UploadStoresSize: 3})

	if status, _, err := upload(t, r, "gcodes", "", "a.gcode", 10); err != nil || status != http.StatusCreated {
		t.Fatalf("status %d, err %v", status, err)
	}

	if size, _ := listedSize(t, r, "gcodes", "a.gcode"); size != 3 {
		t.Fatalf("size = %d", size)
	}
}
