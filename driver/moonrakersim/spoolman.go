package moonrakersim

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"time"
)

const (
	methodServerConfig   = "server.config"
	methodSpoolID        = "server.spoolman.get_spool_id"
	methodSpoolStatus    = "server.spoolman.status"
	methodSpoolProxy     = "server.spoolman.proxy"
	notifyActiveSpoolSet = "notify_active_spool_set"
	notifySpoolStatus    = "notify_spoolman_status_changed"
)

// defaultSyncRate is the seconds Moonraker waits before reporting usage to
// Spoolman when its configuration does not say.
const defaultSyncRate = 5

var spoolPath = regexp.MustCompile(`^/v1/spool/(\d+)$`)

type Spool struct {
	ID   int
	Body map[string]any
}

func ABSSpool(id int) Spool {
	return Spool{ID: id, Body: map[string]any{
		"id": id,
		"filament": map[string]any{
			"material":  "ABS",
			"color_hex": "0020C2",
		},
		"initial_weight":   1000.0,
		"used_weight":      313.13,
		"remaining_weight": 686.87,
	}}
}

// Spoolman is the Spoolman component of a machine. SyncRate 0 means 5 seconds
// and Active 0 means no active spool.
type Spoolman struct {
	SyncRate int
	Active   int
	Spools   []Spool
}

func (s *Spoolman) syncRate() int {
	if s.SyncRate == 0 {
		return defaultSyncRate
	}

	return s.SyncRate
}

func (h *Host) initSpoolman(opts Options) {
	if opts.Spoolman == nil {
		return
	}

	copied := *opts.Spoolman
	copied.Spools = make([]Spool, len(opts.Spoolman.Spools))

	for i, spool := range opts.Spoolman.Spools {
		copied.Spools[i] = Spool{ID: spool.ID, Body: maps.Clone(spool.Body)}
	}

	h.spoolman = &copied
}

// answerSpoolman answers a server.config or server.spoolman call. The second
// result is false for any other method.
func (h *Host) answerSpoolman(id uint64, method string, params json.RawMessage) (map[string]any, bool) {
	if method == methodServerConfig {
		return h.answerConfig(id), true
	}

	if h.spoolman == nil {
		return nil, false
	}

	switch method {
	case methodSpoolID:
		return resultFrame(id, map[string]any{"spool_id": h.activeSpool()}), true
	case methodSpoolStatus:
		return resultFrame(id, map[string]any{
			"spoolman_connected": true,
			"pending_reports":    []any{},
			"spool_id":           h.activeSpool(),
		}), true
	case methodSpoolProxy:
		return h.answerProxy(id, params), true
	default:
		return nil, false
	}
}

func (h *Host) answerConfig(id uint64) map[string]any {
	if h.spoolman == nil {
		return resultFrame(id, map[string]any{"config": map[string]any{}})
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	return resultFrame(id, map[string]any{"config": map[string]any{
		"spoolman": map[string]any{"sync_rate": h.spoolman.syncRate()},
	}})
}

// activeSpool is the active spool id, or nil for none, so it encodes as null.
func (h *Host) activeSpool() any {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.spoolman.Active == 0 {
		return nil
	}

	return h.spoolman.Active
}

func (h *Host) answerProxy(id uint64, params json.RawMessage) map[string]any {
	var sent struct {
		Method string `json:"request_method"`
		Path   string `json:"path"`
	}

	if len(params) == 0 || json.Unmarshal(params, &sent) != nil {
		return errorFrame(id, requestRefused, "Invalid proxy request")
	}

	found := spoolPath.FindStringSubmatch(sent.Path)
	if sent.Method != "GET" || found == nil {
		return errorFrame(id, requestRefused, "Unsupported Spoolman request "+sent.Path)
	}

	want, _ := strconv.Atoi(found[1])

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, spool := range h.spoolman.Spools {
		if spool.ID == want {
			return resultFrame(id, maps.Clone(spool.Body))
		}
	}

	return errorFrame(id, requestRefused, fmt.Sprintf("Spool %d not found", want))
}

// SetActiveSpool makes id the active spool and pushes the notification to
// every open connection. Zero means no spool.
func (h *Host) SetActiveSpool(id int) {
	h.mu.Lock()

	if h.spoolman != nil {
		h.spoolman.Active = id
	}

	open := slices.Clone(h.conns)
	h.mu.Unlock()

	var spoolID any
	if id != 0 {
		spoolID = id
	}

	frame := map[string]any{
		"jsonrpc": jsonrpcVersion,
		"method":  notifyActiveSpoolSet,
		"params":  []any{map[string]any{"spool_id": spoolID}},
	}

	for _, held := range open {
		_ = held.write(frame)
	}
}

func (h *Host) SetSpoolmanConnected(connected bool) {
	h.mu.Lock()
	open := slices.Clone(h.conns)
	h.mu.Unlock()

	frame := map[string]any{
		"jsonrpc": jsonrpcVersion,
		"method":  notifySpoolStatus,
		"params":  []any{map[string]any{"spoolman_connected": connected}},
	}

	for _, held := range open {
		_ = held.write(frame)
	}
}

// QueueUsage adds grams to a spool's used weight and takes them off its
// remaining weight after the sync rate, as Moonraker's report to Spoolman does.
func (h *Host) QueueUsage(id int, grams float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.spoolman == nil {
		return
	}

	delay := time.Duration(h.spoolman.syncRate()) * time.Second

	h.timers = append(h.timers, time.AfterFunc(delay, func() { h.applyUsage(id, grams) }))
}

func (h *Host) applyUsage(id int, grams float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, spool := range h.spoolman.Spools {
		if spool.ID != id {
			continue
		}

		for key, sign := range map[string]float64{"used_weight": 1, "remaining_weight": -1} {
			if value, ok := spool.Body[key].(float64); ok {
				spool.Body[key] = value + sign*grams
			}
		}
	}
}

func (h *Host) Refuse(method, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.refused == nil {
		h.refused = map[string]string{}
	}

	h.refused[method] = message
}

func (h *Host) Allow(method string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.refused, method)
}

func (h *Host) CallTimes(method string) []time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.callTimes[method])
}

// Silence makes the host read the method and send no answer, which is
// Moonraker connected and not answering.
func (h *Host) Silence(method string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.silenced == nil {
		h.silenced = map[string]bool{}
	}

	h.silenced[method] = true
}

func (h *Host) Unsilence(method string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.silenced, method)
}

func (h *Host) Remove(method string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.removed == nil {
		h.removed = map[string]bool{}
	}

	h.removed[method] = true
}

func (h *Host) isSilenced(method string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.silenced[method]
}

func (h *Host) isRemoved(method string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.removed[method]
}

func (h *Host) stopTimers() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, timer := range h.timers {
		timer.Stop()
	}
}
