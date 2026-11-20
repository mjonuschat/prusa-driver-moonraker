package moonrakersim

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"path"
	"slices"
	"strings"
)

// The file methods this host answers.
const (
	methodFilesRoots           = "server.files.roots"
	methodFilesGetDirectory    = "server.files.get_directory"
	methodFilesMetadata        = "server.files.metadata"
	methodFilesPostDirectory   = "server.files.post_directory"
	methodFilesDeleteDirectory = "server.files.delete_directory"
	methodFilesDeleteFile      = "server.files.delete_file"
)

const (
	notifyFilelistChanged = "notify_filelist_changed"
	gcodesRoot            = "gcodes"
	configRoot            = "config"
	gcodesDiskPath        = "/home/pi/printer_data/gcodes"
	configDiskPath        = "/home/pi/printer_data/config"
	defaultFreeSpace      = int64(1 << 30)
	diskHeadroom          = int64(1 << 20)
)

// tinyPNG is a valid 1x1 PNG.
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

// Thumbnail is one preview image. Size is what metadata reports and Body is
// what the HTTP route serves. A nil Body is listed in metadata and answers 404.
type Thumbnail struct {
	Width, Height, Size int
	RelativePath        string
	Body                []byte
}

// FileItem names one file as a file-list notification reports it.
type FileItem struct {
	Root, Path string
	Size       int64
	Modified   float64
}

// FileEvent is one change in a notify_filelist_changed frame.
type FileEvent struct {
	Action       string
	Item, Source *FileItem
}

// entry is one node of the seeded tree, keyed by its path from the root name.
type entry struct {
	dir      bool
	size     int64
	modified float64
}

func (h *Host) initFiles(opts Options) {
	h.noGcodes = opts.NoGcodesRoot
	h.readOnly = opts.ReadOnlyGcodes
	h.free = opts.FreeSpace
	h.uploadDropAfter = opts.UploadDropAfter
	h.uploadStoresSize = opts.UploadStoresSize

	if opts.UploadRefusal != nil {
		refusal := *opts.UploadRefusal
		h.uploadRefusal = &refusal
	}

	if h.free == 0 {
		h.free = defaultFreeSpace
	}

	h.estimatedTime = opts.EstimatedTime

	switch {
	case opts.NoThumbnails:
		h.thumbnails = nil
	case opts.Thumbnails == nil:
		body, _ := base64.StdEncoding.DecodeString(tinyPNG)
		h.thumbnails = []Thumbnail{{
			Width: 300, Height: 300, Size: len(body),
			RelativePath: ".thumbs/benchy-300x300.png", Body: body,
		}}
	default:
		h.thumbnails = slices.Clone(opts.Thumbnails)
	}

	h.tree = map[string]entry{
		"gcodes/benchy.gcode":       {size: 1000, modified: 1700000000.5},
		"gcodes/models":             {dir: true, modified: 1700000050},
		"gcodes/models/part.gcode":  {size: 2000, modified: 1700000100},
		"gcodes/empty":              {dir: true, modified: 1700000060},
		"gcodes/odd name #1?.gcode": {size: 300, modified: 1700000200},
		"gcodes/.thumbs":            {dir: true, modified: 1700000300},
		"gcodes/.hidden.gcode":      {size: 50, modified: 1700000300},
	}
}

// SetFreeSpace changes the free bytes the gcodes disk reports.
func (h *Host) SetFreeSpace(free int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.free = free
}

// Requests is the params of each call to the method, in order.
func (h *Host) Requests(method string) []json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.requests[method])
}

// NotifyFiles pushes one notify_filelist_changed frame per call to every open
// connection.
func (h *Host) NotifyFiles(items ...FileEvent) {
	events := make([]any, 0, len(items))

	for _, item := range items {
		event := map[string]any{"action": item.Action}
		if item.Item != nil {
			event["item"] = item.Item.wire()
		}

		if item.Source != nil {
			event["source_item"] = item.Source.wire()
		}

		events = append(events, event)
	}

	h.mu.Lock()
	open := slices.Clone(h.conns)
	h.mu.Unlock()

	frame := map[string]any{"jsonrpc": jsonrpcVersion, "method": notifyFilelistChanged, "params": events}

	for _, held := range open {
		_ = held.write(frame)
	}
}

// NotifyRaw sends one text frame, as given, to every open connection. It is how
// a case sends a notification the real machine would never spell.
func (h *Host) NotifyRaw(frame string) {
	h.mu.Lock()
	open := slices.Clone(h.conns)
	h.mu.Unlock()

	for _, held := range open {
		_ = held.write(json.RawMessage(frame))
	}
}

func (f *FileItem) wire() map[string]any {
	return map[string]any{"root": f.Root, "path": f.Path, "size": f.Size, "modified": f.Modified}
}

func (h *Host) permissions() string {
	if h.readOnly {
		return "r"
	}

	return "rw"
}

// hasRoot reports whether the root is one this host serves.
func (h *Host) hasRoot(root string) bool {
	return root == configRoot || (root == gcodesRoot && !h.noGcodes)
}

func splitRoot(p string) (string, string) {
	p = strings.Trim(p, "/")
	root, rest, _ := strings.Cut(p, "/")

	return root, rest
}

// resolve cleans a request path and checks its root.
func (h *Host) resolve(p string) (string, string, error) {
	root, rest := splitRoot(p)
	if !h.hasRoot(root) {
		return "", "", refuse("Invalid root path (%s)", p)
	}

	rest = path.Clean("/" + rest)[1:]
	if rest == "" {
		return root, root, nil
	}

	return root, root + "/" + rest, nil
}

func (h *Host) answerFiles(id uint64, method string, params json.RawMessage) map[string]any {
	var sent struct {
		Path     string `json:"path"`
		Filename string `json:"filename"`
		Force    bool   `json:"force"`
	}

	_ = json.Unmarshal(params, &sent)

	h.mu.Lock()
	defer h.mu.Unlock()

	result, err := h.files(method, sent.Path, sent.Filename, sent.Force)
	if err != nil {
		return errorFrame(id, requestRefused, err.Error())
	}

	return resultFrame(id, result)
}

// files runs one file call with h.mu held.
func (h *Host) files(method, p, filename string, force bool) (any, error) {
	switch method {
	case methodFilesRoots:
		roots := []map[string]any{}
		if !h.noGcodes {
			roots = append(roots, map[string]any{"name": gcodesRoot, "path": gcodesDiskPath, "permissions": h.permissions()})
		}

		return append(roots, map[string]any{"name": configRoot, "path": configDiskPath, "permissions": "rw"}), nil
	case methodFilesMetadata:
		return h.metadata(filename)
	}

	root, full, err := h.resolve(p)
	if err != nil {
		return nil, err
	}

	switch method {
	case methodFilesGetDirectory:
		return h.directory(root, full, p)
	case methodFilesPostDirectory:
		if _, exists := h.tree[full]; exists || full == root {
			return nil, refuse("Directory already exists (%s)", p)
		}

		h.tree[full] = entry{dir: true, modified: newDirectoryModified}

		return map[string]any{"item": map[string]any{"root": root, "path": strings.TrimPrefix(full, root+"/")}}, nil
	case methodFilesDeleteDirectory:
		return h.deleteDirectory(root, full, p, force)
	default:
		node, exists := h.tree[full]
		if !exists || node.dir {
			return nil, refuse("File does not exist (%s)", p)
		}

		delete(h.tree, full)

		return map[string]any{"item": map[string]any{"root": root, "path": strings.TrimPrefix(full, root+"/")}}, nil
	}
}

// newDirectoryModified is the modified time given to directories created through the API.
const newDirectoryModified float64 = 1700001000

func (h *Host) directory(root, full, asked string) (any, error) {
	if full != root {
		if node, ok := h.tree[full]; !ok || !node.dir {
			return nil, refuse("Directory does not exist (%s)", asked)
		}
	}

	perms := "rw"
	if root == gcodesRoot {
		perms = h.permissions()
	}

	dirs, files := []any{}, []any{}

	for _, name := range slices.Sorted(maps.Keys(h.tree)) {
		if path.Dir(name) != full {
			continue
		}

		node := h.tree[name]
		if node.dir {
			dirs = append(dirs, map[string]any{"dirname": path.Base(name), "modified": node.modified, "size": 4096, "permissions": perms})
		} else {
			files = append(files, map[string]any{"filename": path.Base(name), "modified": node.modified, "size": node.size, "permissions": perms})
		}
	}

	return map[string]any{
		"dirs": dirs, "files": files,
		"disk_usage": map[string]any{"total": h.free + diskHeadroom, "used": diskHeadroom, "free": h.free},
		"root_info":  map[string]any{"name": root, "permissions": perms},
	}, nil
}

func (h *Host) deleteDirectory(root, full, asked string, force bool) (any, error) {
	if full == root {
		return nil, refuse("Cannot delete root directory (%s)", asked)
	}

	if node, ok := h.tree[full]; !ok || !node.dir {
		return nil, refuse("Directory does not exist (%s)", asked)
	}

	var inside []string

	for name := range h.tree {
		if strings.HasPrefix(name, full+"/") {
			inside = append(inside, name)
		}
	}

	if len(inside) > 0 && !force {
		return nil, refuse("Directory is not empty (%s)", asked)
	}

	for _, name := range inside {
		delete(h.tree, name)
	}

	delete(h.tree, full)

	return map[string]any{"item": map[string]any{"root": root, "path": strings.TrimPrefix(full, root+"/")}}, nil
}

func (h *Host) metadata(filename string) (any, error) {
	full := gcodesRoot + "/" + strings.Trim(filename, "/")

	node, ok := h.tree[full]
	if !ok || node.dir || h.noGcodes {
		return nil, refuse("Metadata not available for %s", filename)
	}

	listed := []any{}
	for _, thumb := range h.thumbnails {
		listed = append(listed, map[string]any{
			"width": thumb.Width, "height": thumb.Height, "size": thumb.Size, "relative_path": thumb.RelativePath,
		})
	}

	reply := map[string]any{"filename": filename, "size": node.size, "modified": node.modified, "thumbnails": listed}
	if h.estimatedTime > 0 {
		reply["estimated_time"] = h.estimatedTime
	}

	return reply, nil
}

// serveThumbnail serves the Body of the thumbnail whose RelativePath matches
// the requested path under the file's directory.
func (h *Host) serveThumbnail(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/server/files/")
	root, under := splitRoot(rest)

	h.mu.Lock()
	var body []byte

	found := false

	if h.hasRoot(root) && root == gcodesRoot {
		for _, thumb := range h.thumbnails {
			if thumb.RelativePath == under || strings.HasSuffix(under, "/"+thumb.RelativePath) {
				body, found = thumb.Body, thumb.Body != nil

				break
			}
		}
	}
	h.mu.Unlock()

	if !found {
		http.NotFound(w, r)

		return
	}

	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(body)
}

// refusal is an error whose text is the capitalised message Moonraker sends.
type refusal string

func (e refusal) Error() string { return string(e) }

func refuse(format string, args ...any) error { return refusal(fmt.Sprintf(format, args...)) }

type UploadRefusal struct {
	Status int
	Body   string
}

// Upload is one upload that finished. Path is the path form field, Name the
// file part's filename and Size the bytes the file part carried.
type Upload struct {
	Root, Path, Name string
	Size             int64
}

func (h *Host) ClearUploadRefusal() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.uploadRefusal = nil
}

func (h *Host) Uploads() []Upload {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.uploads)
}

func writeMoonrakerError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": status, "message": message}})
}

// serveUpload takes POST /server/files/upload. It reads the multipart body as a
// stream and keeps only the size of the file part.
func (h *Host) serveUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMoonrakerError(w, http.StatusMethodNotAllowed, "Method not allowed")

		return
	}

	h.mu.Lock()
	refusal := h.uploadRefusal
	noGcodes, readOnly := h.noGcodes, h.readOnly
	dropAfter, storesSize := h.uploadDropAfter, h.uploadStoresSize
	h.mu.Unlock()

	if refusal != nil {
		// Full duplex lets the answer go out while the request body is unread.
		if err := http.NewResponseController(w).EnableFullDuplex(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return
		}

		w.WriteHeader(refusal.Status)
		_, _ = io.WriteString(w, refusal.Body)

		_ = http.NewResponseController(w).Flush()

		return
	}

	reader, err := r.MultipartReader()
	if err != nil {
		writeMoonrakerError(w, http.StatusBadRequest, "Invalid multipart request")

		return
	}

	fields := map[string]string{}

	var (
		name string
		size int64
		seen bool
	)

	for !seen {
		part, err := reader.NextPart()
		if err != nil {
			writeMoonrakerError(w, http.StatusBadRequest, "No file in the upload")

			return
		}

		if part.FormName() != "file" {
			value, _ := io.ReadAll(io.LimitReader(part, 4096))
			fields[part.FormName()] = string(value)

			continue
		}

		seen = true
		name = part.FileName()

		root := fields["root"]
		if root == "" {
			root = gcodesRoot
		}

		if root != gcodesRoot || noGcodes {
			writeMoonrakerError(w, http.StatusBadRequest, "Invalid root path ("+root+")")

			return
		}

		if readOnly {
			writeMoonrakerError(w, http.StatusForbidden, "Gcodes root is read-only")

			return
		}

		buf := make([]byte, 32*1024)

		for {
			n, readErr := part.Read(buf)
			size += int64(n)

			if dropAfter > 0 && size >= dropAfter {
				if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
					_ = conn.Close()
				}

				return
			}

			if errors.Is(readErr, io.EOF) {
				break
			}

			if readErr != nil {
				// A truncated body is not an upload.
				return
			}
		}
	}

	if name == "" {
		writeMoonrakerError(w, http.StatusBadRequest, "No file name in the upload")

		return
	}

	dir := strings.Trim(path.Clean("/"+fields["path"]), "/")
	rel := path.Join(dir, name)

	stored := size
	if storesSize > 0 {
		stored = storesSize
	}

	h.mu.Lock()
	for parent := dir; parent != ""; parent = path.Dir(parent) {
		key := gcodesRoot + "/" + parent
		if _, exists := h.tree[key]; !exists {
			h.tree[key] = entry{dir: true, modified: newDirectoryModified}
		}

		if path.Dir(parent) == "." {
			break
		}
	}

	h.tree[gcodesRoot+"/"+rel] = entry{size: stored, modified: newDirectoryModified}
	h.uploads = append(h.uploads, Upload{Root: gcodesRoot, Path: dir, Name: name, Size: size})
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"item":   map[string]any{"root": gcodesRoot, "path": rel},
		"action": "create_file",
	})
}
