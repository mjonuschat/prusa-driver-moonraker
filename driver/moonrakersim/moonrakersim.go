// Package moonrakersim is a Moonraker host with no Klipper behind it: it
// answers the JSON-RPC calls the real one answers, over the same websocket, so
// a dialect can be driven end to end without a machine on the bench.
//
// It is a fake written from Moonraker's published API and not a recording of a
// real host. So it proves this project's client and says nothing about whether
// a field Moonraker sends is spelled the way this package spells it. What it is
// for is the half a fake can prove: the framing, the subscription, the
// narrowing a machine's own object list performs, and a command reaching the
// machine and being answered.
//
// The default machine is a complete one: it has pause_resume, an extruder and a
// heated bed, so every verb the moonraker dialect declares is offered. Options
// narrows that, which is how a case watches a verb being withheld.
package moonrakersim

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// websocketPath is where Moonraker serves its JSON-RPC API, and therefore where
// this host serves it too.
const websocketPath = "/websocket"

// readHeaderTimeout bounds the request line and headers of one upgrade, because
// the zero value is no timeout at all.
const readHeaderTimeout = 5 * time.Second

// jsonrpcVersion is the version every frame carries.
const jsonrpcVersion = "2.0"

// The methods this host answers. A method outside this list is answered with
// the error a real Moonraker gives, which is what a dialect calling something
// that is not there has to cope with.
const (
	methodPrinterInfo      = "printer.info"
	methodObjectsList      = "printer.objects.list"
	methodObjectsQuery     = "printer.objects.query"
	methodObjectsSubscribe = "printer.objects.subscribe"
	methodPrintPause       = "printer.print.pause"
	methodPrintResume      = "printer.print.resume"
	methodPrintCancel      = "printer.print.cancel"
	methodGcodeScript      = "printer.gcode.script"
	methodPrintStart       = "printer.print.start"
	methodPrinterRestart   = "printer.restart"
	methodFirmwareRestart  = "printer.firmware_restart"
	methodEmergencyStop    = "printer.emergency_stop"
)

// notifyStatusUpdate is the notification a subscription arrives as.
const notifyStatusUpdate = "notify_status_update"

// methodNotFound is the JSON-RPC code for a method the server does not have.
const methodNotFound = -32601

// requestRefused is the code Moonraker answers a call its printer cannot carry
// out with.
const requestRefused = 400

// The readings the default machine reports. They are a machine that is switched
// on, cold and idle, with the two heater ranges a stock configuration carries.
const (
	roomNozzle  = 24.5
	roomBed     = 23.9
	nozzleMax   = 300.0
	nozzle      = 0.4
	bedMax      = 120.0
	coldest     = 0.0
	klipperTime = 578243.57824499
)

// Options is what kind of machine this host pretends to have. Every zero field
// takes the full machine below.
type Options struct {
	// Hostname is what printer.info reports as the Klipper host's name.
	Hostname string
	// SoftwareVersion is the Klipper version printer.info reports.
	SoftwareVersion string
	// Objects is what printer.objects.list answers. Nil takes DefaultObjects.
	Objects []string
	// Status is one reading of every object, keyed by object then field. Nil
	// takes DefaultStatus.
	Status map[string]map[string]any
	// Refused names the methods this host answers with an error, against the
	// message it answers with. It is how a case reads what a dialect does with a
	// machine that will not do as it is told.
	Refused map[string]string
	// UpgradeStatus, when nonzero, is the HTTP status this host answers every
	// upgrade request with, and no websocket is opened.
	UpgradeStatus int
	// APIKey, when set, is the only X-Api-Key this host accepts. A request
	// without it, the upgrade or any HTTP one, is answered 401, as a Moonraker
	// that requires authorization answers.
	APIKey string
	// Addr is the host:port to bind. Empty binds an ephemeral loopback port,
	// which is what a test wants: two cases running at once must not have to
	// agree about a number.
	//
	// It exists for cmd/moonraker-sim, which a process outside this module runs
	// and then dials, and which is therefore the one caller that may need to say
	// where. Bind an address that is not loopback and this fake answers whatever
	// reaches it, so do not: it authenticates nobody and it is not a machine.
	Addr string

	// NoGcodesRoot leaves the gcodes root out of every answer.
	NoGcodesRoot bool
	// ReadOnlyGcodes reports the gcodes root with permissions "r".
	ReadOnlyGcodes bool
	// FreeSpace is the free bytes the gcodes disk reports. Zero takes 1<<30.
	FreeSpace int64
	// Thumbnails is what file metadata lists and the thumbnail route serves. Nil
	// takes one 300x300 entry.
	Thumbnails []Thumbnail
	// NoThumbnails makes metadata report an empty list. It wins over Thumbnails.
	NoThumbnails bool
	// EstimatedTime is the estimated_time, in seconds, that file metadata
	// reports. Zero leaves the field out, as a file with no estimate does.
	EstimatedTime float64

	// UploadRefusal answers every upload with the given status and body, before
	// the request body is read. ClearUploadRefusal stops it.
	UploadRefusal *UploadRefusal
	// UploadDropAfter closes the connection after this many file bytes and
	// stores nothing. Zero is off.
	UploadDropAfter int64
	// UploadStoresSize is the size an upload is listed with, whatever arrived.
	// Zero is off.
	UploadStoresSize int64

	// Spoolman is the machine's Spoolman component. Nil means it has none, and
	// the Spoolman methods answer method-not-found.
	Spoolman *Spoolman
}

// DefaultObjects is a complete Klipper machine: it can pause, it has a hotend
// and it has a heated bed.
func DefaultObjects() []string {
	return []string{
		"configfile", "display_status", "extruder", "gcode", "gcode_move",
		"heater_bed", "heaters", "pause_resume", "print_stats", "toolhead",
		"virtual_sdcard", "webhooks",
	}
}

// DefaultStatus is one reading of a machine that is switched on and idle, with
// the two heater sections a real configuration dump carries.
func DefaultStatus() map[string]map[string]any {
	return map[string]map[string]any{
		"print_stats": {
			"state": "standby", "filename": "", "print_duration": coldest, "total_duration": coldest,
		},
		"webhooks":       {"state": "ready", "state_message": "Printer is ready"},
		"display_status": {"progress": coldest, "message": nil},
		"extruder":       {"temperature": roomNozzle, "target": coldest, "power": coldest},
		"heater_bed":     {"temperature": roomBed, "target": coldest, "power": coldest},
		"toolhead":       {"position": []any{coldest, coldest, coldest, coldest}, "homed_axes": "", "extruder": "extruder"},
		"gcode_move":     {"speed_factor": 1.0, "extrude_factor": 1.0},
		"configfile": {
			"settings": map[string]any{
				"extruder":   map[string]any{"min_temp": coldest, "max_temp": nozzleMax, "nozzle_diameter": nozzle},
				"heater_bed": map[string]any{"min_temp": coldest, "max_temp": bedMax},
				"printer":    map[string]any{"kinematics": "corexy"},
				"stepper_x":  map[string]any{"position_min": coldest, "position_max": 300.0},
				"stepper_y":  map[string]any{"position_min": -5.0, "position_max": 300.0},
				"stepper_z":  map[string]any{"position_max": 280.0},
			},
		},
	}
}

// Host is a running fake. Close it when the case is over.
type Host struct {
	listener net.Listener
	server   *http.Server
	upgrader websocket.Upgrader

	upgradeStatus int

	hostname string
	version  string
	objects  []string

	mu             sync.Mutex
	status         map[string]map[string]any
	refused        map[string]string
	scriptRefusals map[string]string
	scriptDelay    time.Duration
	conns          []*link
	calls          []string
	scripts        []string
	filenames      []string
	eventTime      float64

	requests      map[string][]json.RawMessage
	tree          map[string]entry
	free          int64
	noGcodes      bool
	readOnly      bool
	thumbnails    []Thumbnail
	estimatedTime float64

	uploadRefusal    *UploadRefusal
	uploadDropAfter  int64
	uploadStoresSize int64
	uploads          []Upload

	spoolman  *Spoolman
	silenced  map[string]bool
	removed   map[string]bool
	callTimes map[string][]time.Time
	timers    []*time.Timer
}

// link is one open connection and the lock that keeps two writers off it. A
// push and an answer are written by different goroutines, and gorilla permits
// one writer at a time.
type link struct {
	conn *websocket.Conn

	mu sync.Mutex
}

func (l *link) write(frame any) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.conn.WriteJSON(frame); err != nil {
		return fmt.Errorf("moonrakersim: write a frame: %w", err)
	}

	return nil
}

// Start binds a loopback port and serves until Close. ctx bounds the bind and
// nothing after it: the host serves for as long as it is open.
func Start(ctx context.Context, opts Options) (*Host, error) {
	var binding net.ListenConfig

	listener, err := binding.Listen(ctx, "tcp", orElse(opts.Addr, "127.0.0.1:0"))
	if err != nil {
		return nil, fmt.Errorf("moonrakersim: bind a port: %w", err)
	}

	host := &Host{
		eventTime: klipperTime,
		listener:  listener,
		hostname:  orElse(opts.Hostname, "voron-bench"),
		version:   orElse(opts.SoftwareVersion, "v0.12.0-284-gfb1b4e39"),
		objects:   opts.Objects,
		status:    opts.Status,
		refused:   maps.Clone(opts.Refused),

		upgradeStatus: opts.UpgradeStatus,
	}

	host.initFiles(opts)
	host.initSpoolman(opts)

	if host.objects == nil {
		host.objects = DefaultObjects()
	}

	if host.status == nil {
		host.status = DefaultStatus()
	}

	mux := http.NewServeMux()
	mux.HandleFunc(websocketPath, host.serve)
	mux.HandleFunc("/server/files/upload", host.serveUpload)
	mux.HandleFunc("/server/files/", host.serveThumbnail)

	host.server = &http.Server{Handler: requireKey(opts.APIKey, mux), ReadHeaderTimeout: readHeaderTimeout}

	go func() { _ = host.server.Serve(listener) }()

	return host, nil
}

// Address is the host and port an endpoint names to reach this machine.
func (h *Host) Address() string { return h.listener.Addr().String() }

// Close stops serving and drops every open connection.
func (h *Host) Close() error {
	h.stopTimers()
	h.HangUp()

	if err := h.server.Close(); err != nil {
		return fmt.Errorf("moonrakersim: stop serving: %w", err)
	}

	return nil
}

// SetEventTime sets the eventtime every reply and pushed update carries.
func (h *Host) SetEventTime(seconds float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.eventTime = seconds
}

func (h *Host) clock() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.eventTime
}

// HangUp drops every open connection, which is the machine losing power rather
// than the fake stopping.
func (h *Host) HangUp() {
	h.mu.Lock()
	dropped := h.conns
	h.conns = nil
	h.mu.Unlock()

	for _, held := range dropped {
		_ = held.conn.Close()
	}
}

// Calls is every method this host was asked for, in the order it was asked.
func (h *Host) Calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.calls)
}

// Called reports whether the method was asked for at least once.
func (h *Host) Called(method string) bool {
	return slices.Contains(h.Calls(), method)
}

// Scripts is every gcode line this host was sent, in order. It is what a case
// reads to see the line a verb became.
func (h *Host) Scripts() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.scripts)
}

// Push sends one status update to every open connection and folds it into what
// a later query answers, which is what a real machine reporting a change does.
func (h *Host) Push(status map[string]map[string]any) {
	h.mu.Lock()

	for object, fields := range status {
		if h.status[object] == nil {
			h.status[object] = map[string]any{}
		}

		maps.Copy(h.status[object], fields)
	}

	open := slices.Clone(h.conns)
	h.mu.Unlock()

	frame := map[string]any{
		"jsonrpc": jsonrpcVersion,
		"method":  notifyStatusUpdate,
		"params":  []any{status, h.clock()},
	}

	for _, held := range open {
		_ = held.write(frame)
	}
}

// serve upgrades one connection and answers calls on it until it ends.
func (h *Host) serve(w http.ResponseWriter, r *http.Request) {
	if code := h.upgradeStatus; code != 0 {
		http.Error(w, http.StatusText(code), code)

		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	open := &link{conn: conn}

	h.mu.Lock()
	h.conns = append(h.conns, open)
	h.mu.Unlock()

	defer func() { _ = conn.Close() }()

	for {
		var request struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			ID     *uint64         `json:"id"`
		}

		if err = conn.ReadJSON(&request); err != nil {
			return
		}

		if request.ID == nil {
			continue
		}

		h.record(request.Method, request.Params)

		if h.restarts(request.Method) {
			return
		}

		if h.isSilenced(request.Method) {
			continue
		}

		if err = open.write(h.answer(*request.ID, request.Method, request.Params)); err != nil {
			return
		}
	}
}

func (h *Host) record(method string, params json.RawMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.calls = append(h.calls, method)

	if h.callTimes == nil {
		h.callTimes = map[string][]time.Time{}
	}

	h.callTimes[method] = append(h.callTimes[method], time.Now())

	if h.requests == nil {
		h.requests = map[string][]json.RawMessage{}
	}

	h.requests[method] = append(h.requests[method], slices.Clone(params))
}

// answer is what this host makes of one call.
func (h *Host) answer(id uint64, method string, params json.RawMessage) map[string]any {
	if h.isRemoved(method) {
		return errorFrame(id, methodNotFound, "Method "+method+" is not available")
	}

	if message, refused := h.refusal(method); refused {
		return errorFrame(id, requestRefused, message)
	}

	switch method {
	case methodPrinterInfo:
		return resultFrame(id, map[string]any{
			"state": "ready", "state_message": "Printer is ready",
			"hostname": h.hostname, "software_version": h.version,
		})
	case methodObjectsList:
		return resultFrame(id, map[string]any{"objects": h.objects})
	case methodObjectsQuery, methodObjectsSubscribe:
		return resultFrame(id, map[string]any{
			"eventtime": h.clock(), "status": h.reading(params),
		})
	case methodGcodeScript:
		script := h.recordScript(params)

		if delay := h.slowScript(); delay > 0 {
			time.Sleep(delay)
		}

		if message, refused := h.scriptRefusal(script); refused {
			return errorFrame(id, requestRefused, message)
		}

		return resultFrame(id, "ok")
	case methodEmergencyStop:
		h.Push(map[string]map[string]any{
			"webhooks": {"state": "shutdown", "state_message": "Shutdown due to M112 command"},
		})

		return resultFrame(id, "ok")
	case methodPrintStart:
		h.recordFilename(params)

		return resultFrame(id, "ok")
	case methodPrintPause, methodPrintResume, methodPrintCancel:
		return resultFrame(id, "ok")
	case methodFilesRoots, methodFilesGetDirectory, methodFilesMetadata,
		methodFilesPostDirectory, methodFilesDeleteDirectory, methodFilesDeleteFile:
		return h.answerFiles(id, method, params)
	default:
		if frame, ok := h.answerSpoolman(id, method, params); ok {
			return frame
		}

		return errorFrame(id, methodNotFound, "Method "+method+" is not available")
	}
}

// Filenames is every filename a printer.print.start carried, in order.
func (h *Host) Filenames() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.filenames)
}

func (h *Host) recordFilename(params json.RawMessage) {
	var sent struct {
		Filename string `json:"filename"`
	}

	if len(params) == 0 || json.Unmarshal(params, &sent) != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.filenames = append(h.filenames, sent.Filename)
}

// restarts reports whether the call restarts Klipper and is not refused. Such a
// call is never answered. The connection closes and the held webhooks state
// returns to ready, as it does when the machine comes back.
func (h *Host) restarts(method string) bool {
	if method != methodPrinterRestart && method != methodFirmwareRestart {
		return false
	}

	if _, refused := h.refusal(method); refused {
		return false
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.status["webhooks"] == nil {
		h.status["webhooks"] = map[string]any{}
	}

	h.status["webhooks"]["state"] = "ready"

	return true
}

// recordScript keeps the gcode line one call carried, so a case can read the
// line rather than only the fact that a call was made.
func (h *Host) recordScript(params json.RawMessage) string {
	var sent struct {
		Script string `json:"script"`
	}

	if len(params) == 0 || json.Unmarshal(params, &sent) != nil {
		return ""
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.scripts = append(h.scripts, sent.Script)

	return sent.Script
}

// RefuseScript makes every later printer.gcode.script whose text contains the
// given string answer an error carrying message. The script is still recorded.
func (h *Host) RefuseScript(contains, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.scriptRefusals == nil {
		h.scriptRefusals = map[string]string{}
	}

	h.scriptRefusals[contains] = message
}

func (h *Host) AllowScripts() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.scriptRefusals = nil
}

// SlowScripts delays every script answer by d, so a case can hold one call
// open while another is sent.
func (h *Host) SlowScripts(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.scriptDelay = d
}

func (h *Host) slowScript() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.scriptDelay
}

func (h *Host) scriptRefusal(script string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for contains, message := range h.scriptRefusals {
		if strings.Contains(script, contains) {
			return message, true
		}
	}

	return "", false
}

func (h *Host) refusal(method string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	message, refused := h.refused[method]

	return message, refused
}

// reading is the status of every object the call asked for, and of nothing
// else. An object this machine has not got is left out, which is what Moonraker
// does with an object Klipper does not have.
func (h *Host) reading(params json.RawMessage) map[string]map[string]any {
	asked := askedFor(params)

	h.mu.Lock()
	defer h.mu.Unlock()

	out := make(map[string]map[string]any, len(asked))

	for _, object := range asked {
		fields, held := h.status[object]
		if !held || !slices.Contains(h.objects, object) {
			continue
		}

		out[object] = maps.Clone(fields)
	}

	return out
}

// askedFor is the object names one query or subscription named.
func askedFor(params json.RawMessage) []string {
	var asked struct {
		Objects map[string]json.RawMessage `json:"objects"`
	}

	if len(params) == 0 || json.Unmarshal(params, &asked) != nil {
		return nil
	}

	return slices.Sorted(maps.Keys(asked.Objects))
}

func resultFrame(id uint64, result any) map[string]any {
	return map[string]any{"jsonrpc": jsonrpcVersion, "id": id, "result": result}
}

func errorFrame(id uint64, code int, message string) map[string]any {
	return map[string]any{
		"jsonrpc": jsonrpcVersion, "id": id,
		"error": map[string]any{"code": code, "message": message},
	}
}

func orElse(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

// requireKey answers 401 to a request whose X-Api-Key is not key. An empty key
// lets every request through.
func requireKey(key string, next http.Handler) http.Handler {
	if key == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != key {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r)
	})
}
