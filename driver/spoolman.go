package moonraker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

var spoolPaths = []string{
	connectorapi.ToolPath(connectorapi.HeadID(0), connectorapi.MaterialAbbreviation),
	connectorapi.ToolPath(connectorapi.HeadID(0), connectorapi.MaterialType),
	connectorapi.ToolPath(connectorapi.HeadID(0), connectorapi.MaterialColor),
	connectorapi.ToolPath(connectorapi.HeadID(0), connectorapi.MaterialFullWeight),
	connectorapi.ToolPath(connectorapi.HeadID(0), connectorapi.ConsumedWeight),
}

// spoolReply holds the fields of a Spoolman spool this driver reads. Weights
// stay as encoded so a number is published unchanged.
type spoolReply struct {
	Filament struct {
		Material        string `json:"material"`
		ColorHex        string `json:"color_hex"`
		MultiColorHexes any    `json:"multi_color_hexes"`
	} `json:"filament"`
	InitialWeight json.RawMessage `json:"initial_weight"`
	UsedWeight    json.RawMessage `json:"used_weight"`
}

func decodeSpool(result json.RawMessage) (spoolReply, error) {
	var spool spoolReply
	if err := json.Unmarshal(result, &spool); err != nil {
		return spoolReply{}, fmt.Errorf("decode spool: %w", err)
	}

	return spool, nil
}

var nullValue = json.RawMessage(`null`)

func spoolPatches(spool spoolReply) []connectorapi.StatePatch {
	values := []json.RawMessage{
		materialValue(spool.Filament.Material),
		materialValue(spool.Filament.Material),
		colourValue(spool),
		weightValue(spool.InitialWeight),
		weightValue(spool.UsedWeight),
	}

	out := make([]connectorapi.StatePatch, len(spoolPaths))
	for i, path := range spoolPaths {
		out[i] = connectorapi.StatePatch{Path: path, Value: values[i]}
	}

	return out
}

func spoolClear() []connectorapi.StatePatch {
	out := make([]connectorapi.StatePatch, len(spoolPaths))
	for i, path := range spoolPaths {
		out[i] = connectorapi.StatePatch{Path: path, Value: nullValue}
	}

	return out
}

func materialValue(material string) json.RawMessage {
	if material == "" {
		return nullValue
	}
	encoded, _ := json.Marshal(material)

	return encoded
}

// colourValue refuses a multi-colour spool, whose primary colour is not known.
func colourValue(spool spoolReply) json.RawMessage {
	if multi, ok := spool.Filament.MultiColorHexes.(string); ok && multi != "" {
		return nullValue
	}
	text := "#" + spool.Filament.ColorHex
	if spool.Filament.ColorHex == "" || !connectorapi.ValidColor(text) {
		return nullValue
	}
	encoded, _ := json.Marshal(text)

	return encoded
}

func weightValue(raw json.RawMessage) json.RawMessage {
	var grams float64
	if len(raw) == 0 || json.Unmarshal(raw, &grams) != nil {
		return nullValue
	}
	if grams < 0 {
		return nullValue
	}

	return raw
}

// The settle after a print. Moonraker reports extrusion to Spoolman at its next
// sync tick, so the refresh waits a tick and a margin, then until nothing is
// pending, for at most spoolTimeout.
const (
	spoolMargin  = time.Second
	spoolPoll    = time.Second
	spoolTimeout = 10 * time.Second

	// defaultSyncRate is Moonraker's own default for sync_rate.
	defaultSyncRate = 5 * time.Second

	// spoolTimeoutCode marks the synthetic error an expired request is
	// answered with. It is private to this package.
	spoolTimeoutCode = -1

	methodNotFoundCode = -32601

	expiredBacklog = 16

	spoolSection = "spoolman"
)

type spoolman struct {
	mu sync.Mutex

	enabled  bool
	syncRate time.Duration
	// generation numbers the latest trigger. A reply carrying an older number
	// is dropped.
	generation uint64
	published  bool
	settle     *time.Timer
	deadline   time.Time
}

func (m *spoolman) isEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.enabled
}

func (m *spoolman) next() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.generation++

	return m.generation
}

func (m *spoolman) current(generation uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.enabled && m.generation == generation
}

func (m *spoolman) stopSettle() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.settle != nil {
		m.settle.Stop()
		m.settle = nil
	}
}

func (m *spoolman) takePublished() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	was := m.published
	m.published = false

	return was
}

func (m *spoolman) markPublished() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.published = true
}

func isMethodNotFound(e *rpcError) bool {
	return e != nil && e.Code == methodNotFoundCode
}

// readSpoolConfig reads whether Moonraker runs the spoolman component, and how
// often it reports usage. A refused call or an unreadable value leaves the
// feature on with the default rate, and the later calls find out.
func (s *Session) readSpoolConfig(ctx context.Context) error {
	s.spool.mu.Lock()
	defer s.spool.mu.Unlock()

	s.spool.syncRate = defaultSyncRate
	s.spool.enabled = true

	var reply json.RawMessage

	if err := s.callSync(ctx, methodServerConfig, nil, &reply); err != nil {
		if errors.Is(err, ErrRefused) {
			return nil
		}

		return err
	}

	var config struct {
		Config map[string]map[string]json.RawMessage `json:"config"`
	}

	if json.Unmarshal(reply, &config) != nil || config.Config == nil {
		return nil
	}

	section, held := config.Config[spoolSection]
	if !held {
		s.spool.enabled = false

		return nil
	}

	if rate, ok := seconds(section["sync_rate"]); ok {
		s.spool.syncRate = rate
	}

	return nil
}

// seconds reads a duration in seconds that Moonraker may spell as a number or
// as text.
func seconds(raw json.RawMessage) (time.Duration, bool) {
	var number float64
	if json.Unmarshal(raw, &number) != nil {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return 0, false
		}

		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}

		number = parsed
	}

	if math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 || number > time.Hour.Seconds() {
		return 0, false
	}

	return time.Duration(number * float64(time.Second)), true
}

type requestRecord struct {
	onReply func(rpcFrame)
	timer   *time.Timer
}

// send writes a request and returns at once. The reader runs onReply with the
// answer, or with a synthetic timeout error after s.timeout, and runs it once.
func (s *Session) send(method string, params any, onReply func(rpcFrame)) error {
	id := s.nextID.Add(1)
	record := &requestRecord{onReply: onReply}

	record.timer = time.AfterFunc(s.timeout, func() {
		select {
		case s.expired <- id:
		case <-s.done:
		}
	})

	s.requests.Store(id, record)

	err := s.write(rpcRequest{JSONRPC: jsonrpcVersion, Method: method, Params: params, ID: id})
	if err != nil {
		s.requests.Delete(id)
		record.timer.Stop()

		return err
	}

	return nil
}

func (s *Session) answerRequest(id uint64, frame rpcFrame) bool {
	held, found := s.requests.LoadAndDelete(id)
	if !found {
		return false
	}

	record, ok := held.(*requestRecord)
	if !ok {
		return true
	}

	record.timer.Stop()
	record.onReply(frame)

	return true
}

func (s *Session) expire(id uint64) {
	held, found := s.requests.LoadAndDelete(id)
	if !found {
		return
	}

	record, ok := held.(*requestRecord)
	if !ok {
		return
	}

	record.onReply(rpcFrame{ID: &id, Error: &rpcError{Code: spoolTimeoutCode, Message: "no answer"}})
}

func (s *Session) refreshSpool() {
	if !s.spool.isEnabled() {
		return
	}

	generation := s.spool.next()

	err := s.send(methodSpoolID, nil, func(frame rpcFrame) {
		if s.spoolFailed(generation, frame) {
			return
		}

		var active struct {
			ID *int `json:"spool_id"`
		}

		if err := json.Unmarshal(frame.Result, &active); err != nil {
			s.failSpool("the active spool is not readable")

			return
		}

		s.followSpool(generation, active.ID)
	})
	if err != nil {
		s.failSpool("the active spool could not be asked for")
	}
}

func (s *Session) adoptSpool(id *int) {
	if !s.spool.isEnabled() {
		return
	}

	s.followSpool(s.spool.next(), id)
}

func (s *Session) followSpool(generation uint64, id *int) {
	if id == nil {
		s.failSpool("no spool is active")

		return
	}

	params := map[string]any{"request_method": "GET", "path": "/v1/spool/" + strconv.Itoa(*id)}

	err := s.send(methodSpoolProxy, params, func(frame rpcFrame) {
		if s.spoolFailed(generation, frame) {
			return
		}

		spool, err := decodeSpool(frame.Result)
		if err != nil {
			s.failSpool("the spool is not readable")

			return
		}

		for _, patch := range spoolPatches(spool) {
			s.report(patch)
		}

		s.spool.markPublished()
	})
	if err != nil {
		s.failSpool("the spool could not be asked for")
	}
}

// spoolFailed handles a reply that is an error or is stale, and reports whether
// the caller should stop. A component that has gone away ends the feature even
// when the reply is stale, because it holds for every later call too.
func (s *Session) spoolFailed(generation uint64, frame rpcFrame) bool {
	if isMethodNotFound(frame.Error) {
		s.disableSpool()

		return true
	}

	if !s.spool.current(generation) {
		return true
	}

	if frame.Error != nil {
		s.failSpool(frame.Error.Message)

		return true
	}

	return false
}

// failSpool removes the spool paths when any are published. It runs on the
// reader.
func (s *Session) failSpool(why string) {
	s.log.Debug("no spool is published", "why", why)

	if !s.spool.takePublished() {
		return
	}

	for _, patch := range spoolClear() {
		s.report(patch)
	}
}

// disableSpool ends the feature for the session and clears what it published.
// It runs on the reader.
func (s *Session) disableSpool() {
	s.spool.mu.Lock()
	s.spool.enabled = false
	s.spool.mu.Unlock()

	s.spool.stopSettle()
	s.failSpool("the machine has no Spoolman")
}

func (s *Session) activeSpoolSet(params json.RawMessage) {
	var carried []struct {
		ID *int `json:"spool_id"`
	}

	if json.Unmarshal(params, &carried) != nil || len(carried) == 0 {
		s.refused.Add(1)

		return
	}

	s.adoptSpool(carried[0].ID)
}

// spoolmanStatusChanged refreshes the spool when Moonraker's link to Spoolman
// comes back. A link that goes away changes nothing, because the next call fails and
// removes the paths.
func (s *Session) spoolmanStatusChanged(params json.RawMessage) {
	var carried []struct {
		Connected bool `json:"spoolman_connected"`
	}

	if json.Unmarshal(params, &carried) != nil || len(carried) == 0 {
		s.refused.Add(1)

		return
	}

	if carried[0].Connected {
		s.refreshSpool()
	}
}

func printEnded(previous, now string) bool {
	switch previous {
	case string(statePrinting), string(statePaused):
	default:
		return false
	}

	switch now {
	case string(stateFinished), string(stateStopped), string(stateError):
		return true
	default:
		return false
	}
}

func (s *Session) settleSpool() {
	if !s.spool.isEnabled() {
		return
	}

	generation := s.spool.next()

	s.spool.mu.Lock()
	defer s.spool.mu.Unlock()

	wait := s.spool.syncRate + spoolMargin
	s.spool.deadline = time.Now().Add(wait + spoolTimeout)

	s.armSettle(generation, wait)
}

// armSettle starts the timer that asks for the spool status. The caller holds
// the lock. The callback only writes a request.
func (s *Session) armSettle(generation uint64, wait time.Duration) {
	if s.spool.settle != nil {
		s.spool.settle.Stop()
	}

	s.spool.settle = time.AfterFunc(wait, func() { s.askSpoolStatus(generation) })
}

// askSpoolStatus runs on the timer goroutine and so writes only a request.
func (s *Session) askSpoolStatus(generation uint64) {
	if !s.spool.current(generation) {
		return
	}

	err := s.send(methodSpoolStatus, nil, func(frame rpcFrame) {
		if s.spoolFailed(generation, frame) {
			return
		}

		var status struct {
			Pending []json.RawMessage `json:"pending_reports"`
		}

		if err := json.Unmarshal(frame.Result, &status); err != nil {
			s.failSpool("the spool status is not readable")

			return
		}

		s.spool.mu.Lock()
		waiting := len(status.Pending) > 0 && time.Now().Before(s.spool.deadline)

		if waiting {
			s.armSettle(generation, spoolPoll)
		}

		s.spool.mu.Unlock()

		if !waiting {
			s.refreshSpool()
		}
	})
	if err != nil {
		s.log.Debug("the spool status could not be asked for", "error", err.Error())
	}
}

func (s *Session) stopSpool() {
	s.spool.stopSettle()

	s.requests.Range(func(key, held any) bool {
		if record, ok := held.(*requestRecord); ok {
			record.timer.Stop()
		}

		s.requests.Delete(key)

		return true
	})
}
