package moonrakersim_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

type rig struct {
	t    *testing.T
	host *moonrakersim.Host
	conn *websocket.Conn
	next int
}

func start(t *testing.T, opts moonrakersim.Options) *rig {
	t.Helper()

	host, err := moonrakersim.Start(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = host.Close() })

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+host.Address()+"/websocket", nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	return &rig{t: t, host: host, conn: conn}
}

type reply struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (r *rig) read() reply {
	r.t.Helper()
	_ = r.conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var out reply
	if err := r.conn.ReadJSON(&out); err != nil {
		r.t.Fatal(err)
	}

	return out
}

func (r *rig) call(method string, params any) reply {
	r.t.Helper()
	r.next++

	err := r.conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": r.next, "method": method, "params": params})
	if err != nil {
		r.t.Fatal(err)
	}

	return r.read()
}

func (r *rig) ok(method string, params any, into any) {
	r.t.Helper()

	got := r.call(method, params)
	if got.Error != nil {
		r.t.Fatalf("%s: %s", method, got.Error.Message)
	}

	if into != nil {
		if err := json.Unmarshal(got.Result, into); err != nil {
			r.t.Fatal(err)
		}
	}
}

type listing struct {
	Dirs  []struct{ Dirname string } `json:"dirs"`
	Files []struct {
		Filename string
		Size     int64
	} `json:"files"`
	DiskUsage struct{ Free int64 } `json:"disk_usage"`
}

func TestTheRootsCallReportsGcodesAndConfig(t *testing.T) {
	r := start(t, moonrakersim.Options{})

	var roots []struct{ Name, Path, Permissions string }

	r.ok("server.files.roots", nil, &roots)

	if len(roots) != 2 || roots[0].Name != "gcodes" || roots[0].Permissions != "rw" || roots[1].Name != "config" {
		t.Fatalf("roots = %+v", roots)
	}
}

func TestGetDirectoryListsTheSeededTree(t *testing.T) {
	r := start(t, moonrakersim.Options{})

	var got listing

	r.ok("server.files.get_directory", map[string]any{"path": "gcodes", "extended": true}, &got)

	names := map[string]bool{}
	for _, d := range got.Dirs {
		names[d.Dirname] = true
	}

	if !names["models"] || !names["empty"] {
		t.Fatalf("dirs = %+v", got.Dirs)
	}

	var size int64

	for _, f := range got.Files {
		if f.Filename == "benchy.gcode" {
			size = f.Size
		}
	}

	if size != 1000 {
		t.Fatalf("benchy size = %d", size)
	}

	if got.DiskUsage.Free != 1<<30 {
		t.Fatalf("free = %d", got.DiskUsage.Free)
	}

	if bad := r.call("server.files.get_directory", map[string]any{"path": "gcodes/nope"}); bad.Error == nil {
		t.Fatal("a missing directory answered")
	}
}

func TestPostDirectoryRefusesAnExistingPath(t *testing.T) {
	r := start(t, moonrakersim.Options{})

	if got := r.call("server.files.post_directory", map[string]any{"path": "gcodes/models"}); got.Error == nil {
		t.Fatal("existing path accepted")
	}

	r.ok("server.files.post_directory", map[string]any{"path": "gcodes/fresh"}, nil)
}

func TestDeleteDirectoryRefusesANonEmptyFolderWithoutForce(t *testing.T) {
	r := start(t, moonrakersim.Options{})

	if got := r.call("server.files.delete_directory", map[string]any{"path": "gcodes/models", "force": false}); got.Error == nil {
		t.Fatal("non-empty folder deleted without force")
	}

	r.ok("server.files.delete_directory", map[string]any{"path": "gcodes/models", "force": true}, nil)
	r.ok("server.files.delete_directory", map[string]any{"path": "gcodes/empty"}, nil)

	if got := r.call("server.files.delete_directory", map[string]any{"path": "gcodes", "force": true}); got.Error == nil {
		t.Fatal("root deleted")
	}
}

func TestDeleteFileRemovesIt(t *testing.T) {
	r := start(t, moonrakersim.Options{})

	r.ok("server.files.delete_file", map[string]any{"path": "gcodes/benchy.gcode"}, nil)

	if got := r.call("server.files.delete_file", map[string]any{"path": "gcodes/benchy.gcode"}); got.Error == nil {
		t.Fatal("deleted twice")
	}

	if got := r.call("server.files.delete_file", map[string]any{"path": "gcodes/models"}); got.Error == nil {
		t.Fatal("deleted a directory as a file")
	}
}

func TestMetadataReportsTheThumbnailList(t *testing.T) {
	r := start(t, moonrakersim.Options{})

	var meta struct {
		Thumbnails []struct {
			Width        int
			RelativePath string `json:"relative_path"`
		}
	}

	r.ok("server.files.metadata", map[string]any{"filename": "benchy.gcode"}, &meta)

	if len(meta.Thumbnails) != 1 || meta.Thumbnails[0].Width != 300 || meta.Thumbnails[0].RelativePath != ".thumbs/benchy-300x300.png" {
		t.Fatalf("thumbnails = %+v", meta.Thumbnails)
	}

	if got := r.call("server.files.metadata", map[string]any{"filename": "ghost.gcode"}); got.Error == nil {
		t.Fatal("metadata for a missing file")
	}

	none := start(t, moonrakersim.Options{NoThumbnails: true, Thumbnails: []moonrakersim.Thumbnail{{Width: 1}}})
	none.ok("server.files.metadata", map[string]any{"filename": "benchy.gcode"}, &meta)

	if len(meta.Thumbnails) != 0 {
		t.Fatalf("thumbnails = %+v", meta.Thumbnails)
	}
}

func get(t *testing.T, host *moonrakersim.Host, p string) (int, []byte) {
	t.Helper()

	resp, err := http.Get("http://" + host.Address() + p)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, body
}

func TestTheThumbnailRouteServesTheBody(t *testing.T) {
	r := start(t, moonrakersim.Options{Thumbnails: []moonrakersim.Thumbnail{
		{Width: 300, Height: 300, Size: 4, RelativePath: ".thumbs/a.png", Body: []byte("body")},
		{Width: 32, Height: 32, Size: 4, RelativePath: ".thumbs/b.png"},
	}})

	if code, body := get(t, r.host, "/server/files/gcodes/.thumbs/a.png"); code != 200 || string(body) != "body" {
		t.Fatalf("got %d %q", code, body)
	}

	if code, _ := get(t, r.host, "/server/files/gcodes/.thumbs/zzz.png"); code != 404 {
		t.Fatalf("unknown path = %d", code)
	}

	if code, _ := get(t, r.host, "/server/files/gcodes/.thumbs/b.png"); code != 404 {
		t.Fatalf("nil body = %d", code)
	}
}

func TestNotifyFilesPushesOneFrame(t *testing.T) {
	r := start(t, moonrakersim.Options{})
	r.ok("printer.info", nil, nil)

	r.host.NotifyFiles(moonrakersim.FileEvent{
		Action: "create_file",
		Item:   &moonrakersim.FileItem{Root: "gcodes", Path: "x.gcode", Size: 5, Modified: 1},
	})

	got := r.read()
	if got.Method != "notify_filelist_changed" {
		t.Fatalf("method = %q", got.Method)
	}

	var params []struct {
		Action string
		Item   struct{ Path string }
	}

	if err := json.Unmarshal(got.Params, &params); err != nil || len(params) != 1 || params[0].Action != "create_file" || params[0].Item.Path != "x.gcode" {
		t.Fatalf("params = %s (%v)", got.Params, err)
	}
}

func TestRequestsReturnsTheParamsOfEachCall(t *testing.T) {
	r := start(t, moonrakersim.Options{Refused: map[string]string{"server.files.delete_file": "File currently in use"}})

	r.ok("server.files.metadata", map[string]any{"filename": "benchy.gcode"}, nil)
	r.ok("server.files.metadata", map[string]any{"filename": "models/part.gcode"}, nil)

	got := r.host.Requests("server.files.metadata")
	if len(got) != 2 || string(got[1]) != `{"filename":"models/part.gcode"}` {
		t.Fatalf("requests = %s", got)
	}

	if bad := r.call("server.files.delete_file", map[string]any{"path": "gcodes/benchy.gcode"}); bad.Error == nil || bad.Error.Message != "File currently in use" {
		t.Fatalf("refusal = %+v", bad.Error)
	}
}

func TestRefuseScriptAnswersAnErrorAndStillRecordsTheScript(t *testing.T) {
	r := start(t, moonrakersim.Options{})
	r.host.RefuseScript("G1", "Must home axis first")

	got := r.call("printer.gcode.script", map[string]any{"script": "G1 X1"})
	if got.Error == nil || got.Error.Message != "Must home axis first" {
		t.Fatalf("error = %+v", got.Error)
	}

	if scripts := r.host.Scripts(); len(scripts) != 1 || scripts[0] != "G1 X1" {
		t.Fatalf("scripts = %v", scripts)
	}

	r.ok("printer.gcode.script", map[string]any{"script": "G28"}, nil)
}

func TestAllowScriptsClearsIt(t *testing.T) {
	r := start(t, moonrakersim.Options{})
	r.host.RefuseScript("G1", "no")
	r.host.AllowScripts()
	r.ok("printer.gcode.script", map[string]any{"script": "G1 X1"}, nil)
}
