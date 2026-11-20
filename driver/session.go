package moonraker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The shipping liveness numbers. Moonraker sends a process reading every second
// of its own accord, so a minute of silence is a connection that has gone away
// rather than a machine with nothing to say.
const (
	readDeadline = 60 * time.Second
	pingInterval = 20 * time.Second
	pingTimeout  = 10 * time.Second
)

// ErrSessionEnded reports a call made on a session whose connection has gone.
var ErrSessionEnded = errors.New("moonraker: the session has ended")

// liveness is how long a connection may say nothing, how often the host asks it
// to say something, and how long that asking may take.
//
// A link that blackholes rather than resets leaves a socket that still accepts
// writes, so the ping proves nothing on its own and the read deadline is what
// turns the silence into a disconnection. The zero value takes the shipping
// numbers.
type liveness struct {
	readDeadline time.Duration
	pingInterval time.Duration
	pingTimeout  time.Duration
}

func (l liveness) orShipping() liveness {
	if l.readDeadline <= 0 {
		l.readDeadline = readDeadline
	}

	if l.pingInterval <= 0 {
		l.pingInterval = pingInterval
	}

	if l.pingTimeout <= 0 {
		l.pingTimeout = pingTimeout
	}

	return l
}

// Session is one connected Klipper machine.
//
// It has two lives. During the identity exchange the goroutine inside Connect
// reads the connection itself, because there is nothing to hand a reply to yet.
// After begin, one reader goroutine owns every read, and it is the only writer
// to the patch channel and therefore the only thing that closes it.
type Session struct {
	// motion serialises the motion verbs and the verbs that change or depend
	// on gcode state, so a jog and its recovery restore run as one unit and a
	// resume cannot run between them.
	motion sync.Mutex

	// levelling is the gcode command that levels this machine, or empty when
	// it lists no levelling object. It is set during the handshake.
	levelling string

	conn    *websocket.Conn
	timeout time.Duration
	live    liveness
	log     *slog.Logger

	// reported is what printer.info answered, kept so Connect can name the
	// machine after the exchange it performed.
	reported printerInfo
	// asked is the objects the subscription covers, which is also what a status
	// query asks for.
	asked []string
	// options is the role options of the endpoint, set before the handshake.
	options roleOptions
	roles   roles

	identity connectorapi.Identity
	caps     connectorapi.Set
	// probe is what this machine answered about itself during the handshake:
	// the verbs the objects it reports can carry out, with the bounds its own
	// configuration declares. The host reads it through
	// connectorapi.ReportsProbe and narrows the set it commands against to it.
	probe connectorapi.Set

	// files is the storage this machine reports, which a later file event
	// marks stale.
	files filesState

	rescans rescanQueue

	unit    string
	folders *folderMemory
	paths   *pathMemory

	// httpBase and fetch reach Moonraker's HTTP side, which serves thumbnails.
	httpBase string
	fetch    *fetcher
	// upload sends files, which a client with a timeout would cut short.
	upload  *http.Client
	sending atomic.Bool

	// reader gives each Klipper print an id, which the machine does not, and
	// remembers which head prints. See telemetry.go.
	reader reader

	// first is the reading the subscription answered with. The reader emits it
	// before its loop, so that the patch channel has exactly one writer.
	first []connectorapi.StatePatch

	patches chan connectorapi.StatePatch
	changes chan connectorapi.FileChange
	// done closes when the reader has gone, so a call waiting on a reply that
	// can no longer arrive is released.
	done chan struct{}

	nextID atomic.Uint64

	writing sync.Mutex

	pending sync.Map

	closing sync.Once

	// machineState is the last /state word the machine reported, nil until it
	// has reported one.
	machineState atomic.Pointer[string]

	spool    spoolman
	requests sync.Map
	// expired carries the id of a request whose timer fired to the reader,
	// which is the only goroutine allowed to act on it.
	expired chan uint64

	dropped atomic.Uint64
	refused atomic.Uint64
}

var _ connectorapi.Session = (*Session)(nil)

// The host reads what this connection has cost through connectorapi.CountsLink.
// A session that answers neither method is reported as counting nothing.
var _ connectorapi.CountsLink = (*Session)(nil)

// This machine says what it has, so the host holds it to that and not to what
// this driver's code can do for a Klipper machine in general.
var _ connectorapi.ReportsProbe = (*Session)(nil)

// newSession wraps a connection that is open and has said nothing yet.
func newSession(conn *websocket.Conn, timeout time.Duration, live liveness, log *slog.Logger) *Session {
	return &Session{
		conn:    conn,
		timeout: timeout,
		live:    live,
		log:     log,
		patches: make(chan connectorapi.StatePatch, patchBacklog),
		changes: make(chan connectorapi.FileChange, filesChangedDepth),
		done:    make(chan struct{}),
		expired: make(chan uint64, expiredBacklog),
	}
}

// handshake performs the identity exchange and the live probe, and returns the
// capability set the machine's own answer narrowed the offer to.
//
// Every read here is made by the caller's goroutine. The reader goroutine has
// not started, so a frame that is not the reply being waited for is skipped
// rather than dispatched.
func (s *Session) handshake(ctx context.Context, offered connectorapi.Set) (connectorapi.Set, error) {
	if err := s.callSync(ctx, methodPrinterInfo, nil, &s.reported); err != nil {
		return nil, err
	}

	if s.reported.Hostname == "" {
		return nil, fmt.Errorf(
			"%w: printer.info named no hostname, so this machine has nothing to be called", ErrUnreachable)
	}

	var listed objectList
	if err := s.callSync(ctx, methodObjectsList, nil, &listed); err != nil {
		return nil, err
	}

	extruders := extrudersOf(listed.Objects)
	s.asked = append(present(subscribed, listed.Objects), furtherExtruders(extruders)...)

	available, err := s.readHeaters(ctx, listed.Objects)
	if err != nil {
		return nil, err
	}

	s.roles = resolveRoles(listed.Objects, available, s.options)
	for _, miss := range s.roles.unresolved {
		s.log.Warn("an option names an object the machine does not list",
			"option", miss.option, "value", miss.value)
	}

	for _, object := range s.roles.objects() {
		if !slices.Contains(s.asked, object) {
			s.asked = append(s.asked, object)
		}
	}

	s.reader.gauges.roles = s.roles

	dumped, err := s.readConfig(ctx, listed.Objects)
	if err != nil {
		return nil, err
	}

	heaters := heaterBounds(dumped, s.roles.chamber)

	// Storage is read before subscribing: callSync drops notifications while it
	// waits, so no status update may be in flight yet.
	gcodes, err := s.readStorage(ctx)
	if err != nil {
		return nil, err
	}

	if err = s.readSpoolConfig(ctx); err != nil {
		return nil, err
	}

	var opening statusReply
	if err = s.callSync(ctx, methodObjectsSubscribe, objectsParam(s.asked), &opening); err != nil {
		return nil, err
	}

	s.reader.heads.count = len(extruders)
	opened := s.reader.patchesFrom(opening.Status, opening.EventTime, time.Now())

	var remembered []string
	if s.paths != nil {
		remembered = s.paths.recorded(s.unit)
		s.paths.remember(s.unit, s.reader.gauges.written)
	}

	s.first = sweepPatches(sweepPaths(len(extruders), remembered, s.reader.gauges.written))
	s.first = append(s.first, toolPatches(dumped, extruders)...)
	s.first = append(s.first, buildVolumePatches(dumped)...)
	s.first = append(s.first, opened...)

	for _, patch := range opened {
		s.noteState(patch)
	}

	var both bool

	s.levelling, both = levelCommandOf(listed.Objects)
	if both {
		s.log.Warn("the machine lists z_tilt and quad_gantry_level, so Level bed runs "+zTiltScript,
			"command", s.levelling)
	}

	s.probe = probe(listed.Objects, heaters, len(extruders), gcodes, s.roles.chamber, motionLimitsOf(dumped), offered)

	return offered.Intersect(s.probe), nil
}

// readConfig asks the machine for its own configuration: the bounds of its
// heaters, the nozzle of each extruder and the travel of each axis. A machine that reports no
// configfile object answers an empty configuration, and the ceiling this
// driver declares stands.
func (s *Session) readConfig(ctx context.Context, available []string) (configDump, error) {
	if !slices.Contains(available, objectConfigFile) {
		return configDump{}, nil
	}

	var reply statusReply

	params := map[string]any{"objects": map[string]any{objectConfigFile: []string{"settings"}}}
	if err := s.callSync(ctx, methodObjectsQuery, params, &reply); err != nil {
		return configDump{}, err
	}

	var dumped configDump
	if err := decodeResult(methodObjectsQuery, reply.Status[objectConfigFile], &dumped); err != nil {
		return configDump{}, err
	}

	return dumped, nil
}

// readHeaters asks the machine which heaters it has. A machine that lists no
// heaters object answers none, so it resolves no chamber heater.
func (s *Session) readHeaters(ctx context.Context, available []string) ([]string, error) {
	if !slices.Contains(available, objectHeaters) {
		return nil, nil
	}

	var reply statusReply

	params := map[string]any{"objects": map[string]any{objectHeaters: []string{"available_heaters"}}}
	if err := s.callSync(ctx, methodObjectsQuery, params, &reply); err != nil {
		return nil, err
	}

	raw, held := reply.Status[objectHeaters]
	if !held {
		return nil, nil
	}

	var heaters heatersList
	if err := decodeResult(methodObjectsQuery, raw, &heaters); err != nil {
		return nil, err
	}

	return heaters.Available, nil
}

// furtherExtruders is every extruder but the first, which subscribed already
// names.
func furtherExtruders(extruders []string) []string {
	return slices.DeleteFunc(slices.Clone(extruders), func(object string) bool { return object == objectExtruder })
}

// present is the objects this dialect wants that the machine reports, in the
// order this dialect names them.
func present(wanted, available []string) []string {
	out := make([]string, 0, len(wanted))

	for _, object := range wanted {
		if slices.Contains(available, object) {
			out = append(out, object)
		}
	}

	return out
}

// begin fixes what the session reports and starts the goroutines that own the
// connection from here on.
func (s *Session) begin(identity connectorapi.Identity, caps connectorapi.Set) {
	s.identity = identity
	s.caps = caps
	s.unit = identity.UnitUID

	if s.folders != nil {
		recorded := s.folders.recorded(s.unit)
		if len(recorded) > 0 {
			s.changes <- rescanOf(gcodesRoot)

			for _, folder := range recorded {
				if folder != gcodesRoot {
					s.changes <- rescanOf(folder)
				}
			}
		}
	}

	go s.read()
	go s.keepalive()
}

// Identity is what this machine is called.
func (s *Session) Identity() connectorapi.Identity { return s.identity }

// Capabilities is the resolved set and its epoch.
//
// The set is copied out, because the contract promises it does not move within
// an epoch and a caller handed the live map can move it.
//
// The epoch is 1 and never changes. This dialect does not renegotiate: a
// machine whose configuration changed is reached again as a new session.
func (s *Session) Capabilities() (connectorapi.Set, uint64) { return s.caps.Clone(), 1 }

// Probe is what this machine answered about itself, which is the third tier of
// the capability model. It is nil until the handshake has run.
//
// The keys it carries are the verbs the objects this machine reports can carry
// out. A verb it leaves out is one no object behind it exists for, so the
// machine would answer the call with an error.
func (s *Session) Probe() connectorapi.Set { return s.probe.Clone() }

// State is the addressable state tree's update channel. It closes when the
// connection ends, whichever end ended it.
func (s *Session) State() <-chan connectorapi.StatePatch { return s.patches }

// Dropped is how many state patches this session discarded because the host was
// not reading them.
func (s *Session) Dropped() uint64 { return s.dropped.Load() }

// Refused is how many frames the machine sent that this dialect could not read.
func (s *Session) Refused() uint64 { return s.refused.Load() }

// Close releases the session. It is safe to call more than once, which is what
// a host shutting down after an error needs.
//
// Closing the connection is what ends the reader, and the reader is what closes
// the patch channel. So there is one path out and no way for a second Close to
// close the channel twice.
func (s *Session) Close() error {
	s.hangUp()

	return nil
}

// hangUp drops the connection. It is separate from Close so that a handshake
// that failed lets go of the socket without promising anything about a patch
// channel no reader ever owned.
func (s *Session) hangUp() {
	s.closing.Do(func() { _ = s.conn.Close() })
}

// Do sends one command to the machine and reports what it made of it.
//
// The arguments were validated against Capabilities and the declared defaults
// filled in before this was reached, so nothing here re-checks a value this
// driver declared.
func (s *Session) Do(ctx context.Context, cmd connectorapi.Command) (connectorapi.Result, error) {
	// The host refuses a verb this machine does not have before it reaches
	// here: it reads Probe when it takes the session and holds the machine to
	// it. This is the last line rather than the first, and it is here because a
	// driver that answers for a verb nothing declared reports an outcome that
	// did not happen, and no caller can tell that apart from one that did.
	if !s.caps.Has(cmd.Verb) {
		return connectorapi.Result{}, connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", cmd.Verb.String()))
	}

	if takesMotionLock(cmd.Verb) {
		s.motion.Lock()
		defer s.motion.Unlock()
	}

	if err := s.refuseInState(cmd.Verb); err != nil {
		return connectorapi.Result{}, err
	}

	method, params, err := s.callFor(cmd)
	if err != nil {
		return connectorapi.Result{}, err
	}

	result, refusal, err := s.callWithin(ctx, method, params, waitFor(cmd.Verb, s.timeout))
	if err != nil {
		// Klipper restarts by hanging up, so the lost connection is the answer.
		if restarts(cmd.Verb) && errors.Is(err, ErrSessionEnded) {
			return connectorapi.Result{OK: true}, nil
		}

		return connectorapi.Result{}, err
	}

	if refusal != nil {
		if isJog(cmd.Verb) {
			// Klipper skips the restore after an error, which would leave the
			// machine in relative mode. This is best effort, so the outcome is ignored.
			_, _, _ = s.call(ctx, methodGcodeScript, map[string]any{"script": restoreJogScript})
		}

		return connectorapi.Result{OK: false, MachineReason: refusal.Message}, nil
	}

	return connectorapi.Result{OK: true, Raw: result}, nil
}

// refuseInState is the guard on a verb the machine's last reported state does
// not accept. A session that has heard no state accepts everything, because
// nothing says it should not.
func (s *Session) refuseInState(verb connectorapi.Capability) error {
	now := s.machineState.Load()
	if now == nil {
		if isMotion(verb) {
			return fault.PrinterVerbNotAllowedInState.
				New("the printer state is not known yet, and " + verb.String() + " is not sent until it is").
				With(fault.StringParam("verb", verb.String()))
		}

		return nil
	}

	state := connectorapi.StateName(*now)
	if states().Accepted(state, verb) {
		return nil
	}

	return fault.PrinterVerbNotAllowedInState.
		New("the printer reported "+*now+", and "+verb.String()+" is not sent in that state").
		With(fault.StringParam("verb", verb.String()), fault.StringParam("state", *now))
}

// callFor is the call one command becomes: a Moonraker method of its own, a
// status query, or a gcode line.
func (s *Session) callFor(cmd connectorapi.Command) (method string, params any, err error) {
	if cmd.Verb == VerbSendStateInfo {
		return methodObjectsQuery, objectsParam(s.asked), nil
	}

	if cmd.Verb == VerbStartPrint {
		relative, err := gcodesPath(cmd.Args["path"])
		if err != nil {
			return "", nil, err
		}

		if relative == "" {
			return "", nil, connectorapi.PathNotAllowed.New("the gcodes root is not a file")
		}

		return methodPrintStart, map[string]any{"filename": relative}, nil
	}

	switch cmd.Verb {
	case VerbMeshBedLeveling:
		if s.levelling == "" {
			return "", nil, connectorapi.CapabilityNotSupported.
				New("the printer lists no levelling object").
				With(fault.StringParam("verb", cmd.Verb.String()))
		}

		return methodGcodeScript, map[string]any{"script": s.levelling}, nil
	case VerbSendGCode:
		line, lineErr := gcodeLine(cmd.Args[gcodeArgName])
		if lineErr != nil {
			return "", nil, lineErr
		}

		return methodGcodeScript, map[string]any{"script": line}, nil
	case VerbForceGCode:
		if stopErr := emergencyStop(cmd.Args[gcodeArgName]); stopErr != nil {
			return "", nil, stopErr
		}

		return methodEmergencyStop, nil, nil
	}

	if named, ok := methodFor(cmd.Verb); ok {
		return named, nil, nil
	}

	script, err := gcodeFor(cmd, s.roles.chamber.object)
	if err != nil {
		return "", nil, err
	}

	return methodGcodeScript, map[string]any{"script": script}, nil
}

// call sends one request and waits for the machine's answer. refusal is the
// machine's own error, which is an answer and not a failure of this connection.
func (s *Session) call(
	ctx context.Context, method string, params any,
) (result json.RawMessage, refusal *rpcError, err error) {
	return s.callWithin(ctx, method, params, s.timeout)
}

// callWithin is call with its own wait for the answer.
func (s *Session) callWithin(
	ctx context.Context, method string, params any, wait time.Duration,
) (result json.RawMessage, refusal *rpcError, err error) {
	// Before the write and not only around the wait. A caller who has already
	// given up must not move the machine, and a pause the caller cancelled is
	// still a pause once it is on the wire.
	if err = ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("moonraker: %s: %w", method, err)
	}

	id := s.nextID.Add(1)
	replies := make(chan rpcFrame, 1)

	s.pending.Store(id, replies)
	defer s.pending.Delete(id)

	if err = s.write(rpcRequest{JSONRPC: jsonrpcVersion, Method: method, Params: params, ID: id}); err != nil {
		return nil, nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	select {
	case frame := <-replies:
		return frame.Result, frame.Error, nil
	case <-s.done:
		return nil, nil, fmt.Errorf("%w: %s was never answered", ErrSessionEnded, method)
	case <-callCtx.Done():
		return nil, nil, fmt.Errorf("moonraker: %s: %w", method, callCtx.Err())
	}
}

// callSync sends one request and reads the connection itself until the answer
// arrives. It is the handshake's call, and it is used before any goroutine owns
// the reads.
func (s *Session) callSync(ctx context.Context, method string, params any, into any) error {
	id := s.nextID.Add(1)

	if err := s.write(rpcRequest{JSONRPC: jsonrpcVersion, Method: method, Params: params, ID: id}); err != nil {
		return err
	}

	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > s.timeout {
		deadline = time.Now().Add(s.timeout)
	}

	if err := s.conn.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("%w: arm the read deadline: %w", ErrUnreachable, err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("moonraker: %s: %w", method, err)
		}

		var frame rpcFrame
		if err := s.conn.ReadJSON(&frame); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrUnreachable, method, err)
		}

		if frame.ID == nil || *frame.ID != id {
			continue
		}

		if frame.Error != nil {
			return fmt.Errorf("%w: %s: %s", ErrRefused, method, frame.Error.Message)
		}

		if into == nil {
			return nil
		}

		return decodeResult(method, frame.Result, into)
	}
}

// write puts one request on the wire. The mutex is what makes a command sent
// from one goroutine and a ping sent from another two whole frames.
func (s *Session) write(request rpcRequest) error {
	s.writing.Lock()
	defer s.writing.Unlock()

	if err := s.conn.SetWriteDeadline(time.Now().Add(s.timeout)); err != nil {
		return fmt.Errorf("%w: arm the write deadline: %w", ErrUnreachable, err)
	}

	if err := s.conn.WriteJSON(request); err != nil {
		return fmt.Errorf("%w: send %s: %w", ErrUnreachable, request.Method, err)
	}

	return nil
}

// read owns every read once the session has begun. It is the only writer to the
// patch channel, which is what lets it be the only closer.
func (s *Session) read() {
	defer close(s.patches)
	defer close(s.done)
	defer s.hangUp()
	defer s.rememberPaths()

	s.conn.SetPongHandler(func(string) error { return s.rearm() })

	if err := s.rearm(); err != nil {
		return
	}

	for _, patch := range s.first {
		s.report(patch)
	}

	defer s.stopSpool()

	if s.spool.isEnabled() {
		s.refreshSpool()
	}

	incoming := s.readFrames()

	for {
		select {
		case message := <-incoming:
			if message.err != nil {
				s.log.Debug("the machine's connection ended", "error", message.err.Error())

				return
			}

			if err := s.rearm(); err != nil {
				return
			}

			s.dispatch(message.data)
		case id := <-s.expired:
			s.expire(id)
		}
	}
}

type inbound struct {
	data []byte
	err  error
}

// readFrames moves the blocking read to its own goroutine, so that read can also
// act on an expired request. The goroutine ends with the connection, and it
// never writes to the patch channel.
func (s *Session) readFrames() <-chan inbound {
	out := make(chan inbound)

	go func() {
		for {
			_, data, err := s.conn.ReadMessage()

			select {
			case out <- inbound{data: data, err: err}:
			case <-s.done:
				return
			}

			if err != nil {
				return
			}
		}
	}()

	return out
}

func (s *Session) rememberPaths() {
	if s.paths != nil {
		s.paths.remember(s.unit, s.reader.gauges.written)
	}
}

// rearm pushes the read deadline out to the full silence this dialect tolerates.
func (s *Session) rearm() error {
	if err := s.conn.SetReadDeadline(time.Now().Add(s.live.readDeadline)); err != nil {
		return fmt.Errorf("%w: arm the read deadline: %w", ErrUnreachable, err)
	}

	return nil
}

// dispatch acts on one frame: an answer to a call, a subscription update, or
// something this dialect has no reading of.
func (s *Session) dispatch(data []byte) {
	var frame rpcFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		s.refused.Add(1)

		return
	}

	if frame.ID != nil {
		s.answer(*frame.ID, frame)

		return
	}

	switch frame.Method {
	case notifyStatusUpdate:
		s.update(frame.Params)
	case notifyFilelistChanged:
		s.filesChanged(frame.Params)
	case notifyActiveSpoolSet:
		s.activeSpoolSet(frame.Params)
	case notifySpoolmanStatusChanged:
		s.spoolmanStatusChanged(frame.Params)
	}
}

// answer hands one reply to the call that is waiting for it. A reply nobody is
// waiting for is dropped: the caller gave up, and the channel it left behind is
// gone with it.
func (s *Session) answer(id uint64, frame rpcFrame) {
	if s.answerRequest(id, frame) {
		return
	}

	waiting, found := s.pending.Load(id)
	if !found {
		return
	}

	replies, ok := waiting.(chan rpcFrame)
	if !ok {
		return
	}

	select {
	case replies <- frame:
	default:
	}
}

// update turns one subscription notification into patches. Its params are an
// array whose first element is the changed status.
func (s *Session) update(params json.RawMessage) {
	var carried []json.RawMessage
	if json.Unmarshal(params, &carried) != nil || len(carried) == 0 {
		s.refused.Add(1)

		return
	}

	var status map[string]json.RawMessage
	if json.Unmarshal(carried[0], &status) != nil {
		s.refused.Add(1)

		return
	}

	// A missing or non-numeric event time stays 0, which makes the reader use the
	// driver's clock, so a failed parse is not an error here.
	var eventTime float64
	if len(carried) > 1 {
		_ = json.Unmarshal(carried[1], &eventTime)
	}

	for _, patch := range s.reader.patchesFrom(status, eventTime, time.Now()) {
		s.report(patch)
	}

	s.askEstimate()
}

// askEstimate asks for the estimate of the file now printing, once. It runs on
// the reader goroutine and so only writes the request. The reader runs the
// reply, which is why the estimate needs no lock.
func (s *Session) askEstimate() {
	file, wanted := s.reader.prints.wantEstimate()
	if !wanted {
		return
	}

	err := s.send(methodGetMetadata, map[string]any{"filename": file}, func(frame rpcFrame) {
		var reply metadataReply
		if frame.Error != nil || json.Unmarshal(frame.Result, &reply) != nil {
			return
		}

		s.reader.prints.estimate, s.reader.prints.estimateFile = reply.EstimatedTime, file
	})
	if err != nil {
		s.log.Debug("the estimate of the file printing could not be asked for", "error", err.Error())
	}
}

// report queues one patch for the host. It never waits, because the goroutine
// that calls it is the one reading the connection: a send that waited would
// hold up the reply to a command as well, and every command behind it.
//
// A machine reporting faster than the host reads costs a fixed amount of
// memory, so the backlog gives way and not the process. The oldest patch is
// what goes, which is connectorapi.Session.State's rule: telemetry is a
// repeating snapshot, and a driver that dropped the newest would hold the
// host's tree at a stale reading for as long as the host stayed behind.
func (s *Session) report(patch connectorapi.StatePatch) {
	s.noteState(patch)

	select {
	case s.patches <- patch:
		return
	case <-s.done:
		return
	default:
	}

	select {
	case <-s.patches:
		s.dropped.Add(1)
	default:
	}

	select {
	case s.patches <- patch:
	case <-s.done:
	default:
		s.dropped.Add(1)
	}
}

// keepalive asks the machine to prove the connection is still there, until the
// connection ends.
//
// Without it a machine that lost power holds a session that looks connected:
// the write into a blackholed socket does not fail for as long as the operating
// system keeps retrying, so nothing ends the session and nothing redials.
func (s *Session) keepalive() {
	ticker := time.NewTicker(s.live.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if err := s.ping(); err != nil {
				s.log.Debug("the machine would not take a ping, so its connection is gone",
					"error", err.Error())

				// read is what closes the channels, so ending the connection
				// here leaves exactly one path out.
				s.hangUp()

				return
			}
		}
	}
}

// ping writes one control frame. It takes the write lock because a control
// frame and a request must not interleave on one connection.
func (s *Session) ping() error {
	s.writing.Lock()
	defer s.writing.Unlock()

	if err := s.conn.WriteControl(
		websocket.PingMessage, nil, time.Now().Add(s.live.pingTimeout),
	); err != nil {
		return fmt.Errorf("%w: send a ping: %w", ErrUnreachable, err)
	}

	return nil
}

// noteState keeps the /state word a patch carries for the verb guard.
func (s *Session) noteState(patch connectorapi.StatePatch) {
	var word string
	if patch.Path != statePath || json.Unmarshal(patch.Value, &word) != nil {
		return
	}

	previous := s.machineState.Swap(&word)
	if previous != nil && printEnded(*previous, word) {
		s.settleSpool()
	}
}
