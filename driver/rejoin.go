package moonraker

// What the session does while Klipper restarts.
//
// A RESTART or FIRMWARE_RESTART, sent from here or from Mainsail, Fluidd or a
// macro, ends Moonraker's link to Klipper and not the websocket to Moonraker.
// Moonraker sends notify_klippy_disconnected, drops every object subscription
// and waits for Klipper to start again. Without a new subscription the session
// would stay connected and hear nothing, and the machine would read as it was
// before the restart until the connection dropped for some other reason.
//
// So the session reports BUSY, asks printer.info once a second until Klipper is
// ready, shut down or in error, and subscribes again. A Klipper that comes back
// with a different object list had its configuration changed: the probe built
// at the handshake no longer holds, so the session hangs up and the host dials
// the machine again for a fresh one.
//
// Everything here runs on the reader goroutine, through send, which is what
// keeps the reader the one writer to the patch channel.

import (
	"encoding/json"
	"slices"
	"time"
)

// rejoinPoll is how often a starting Klipper is asked whether it is up.
const rejoinPoll = time.Second

// klippyGone is notify_klippy_disconnected: the machine reads BUSY and the
// session starts waiting for Klipper.
func (s *Session) klippyGone() {
	s.update(json.RawMessage(`[{"webhooks":{"state":"` + klipperStartup + `"}}]`))

	if s.rejoining {
		return
	}

	s.rejoining = true
	s.log.Info("Klipper disconnected from Moonraker; waiting for it to start again")
	s.scheduleRejoin()
}

// askKlippy asks printer.info whether Klipper has finished starting.
func (s *Session) askKlippy() {
	if !s.rejoining || s.rejoinAsking {
		return
	}

	s.rejoinAsking = true

	err := s.send(methodPrinterInfo, nil, func(frame rpcFrame) {
		// A refusal is Moonraker with no Klipper to ask yet: it answers
		// "Klippy Disconnected" for the first moments of a restart.
		var info printerInfo
		if frame.Error != nil || json.Unmarshal(frame.Result, &info) != nil {
			s.scheduleRejoin()

			return
		}

		switch info.State {
		case "ready", "shutdown", "error":
			s.relist()
		default:
			s.scheduleRejoin()
		}
	})
	if err != nil {
		s.log.Debug("Klipper could not be asked whether it is up", "error", err.Error())
		s.scheduleRejoin()
	}
}

// relist compares the machine's object list with the one the handshake read,
// and subscribes again when nothing changed.
func (s *Session) relist() {
	err := s.send(methodObjectsList, nil, func(frame rpcFrame) {
		var listed objectList
		if frame.Error != nil || json.Unmarshal(frame.Result, &listed) != nil {
			s.scheduleRejoin()

			return
		}

		if !sameObjects(s.listed, listed.Objects) {
			s.log.Info("Klipper came back with a different configuration; dialling it again")
			s.hangUp()

			return
		}

		s.resubscribe()
	})
	if err != nil {
		s.scheduleRejoin()
	}
}

// resubscribe asks for the same objects the handshake subscribed to, and
// reports the reading the subscription answers with.
func (s *Session) resubscribe() {
	err := s.send(methodObjectsSubscribe, objectsParam(s.asked), func(frame rpcFrame) {
		var opening statusReply
		if frame.Error != nil || json.Unmarshal(frame.Result, &opening) != nil {
			s.scheduleRejoin()

			return
		}

		s.rejoining, s.rejoinAsking = false, false
		s.log.Info("Klipper is back; subscribed again")

		for _, patch := range s.reader.patchesFrom(opening.Status, opening.EventTime, time.Now()) {
			s.report(patch)
		}

		s.askEstimate()
	})
	if err != nil {
		s.scheduleRejoin()
	}
}

// scheduleRejoin asks again after rejoinPoll, without blocking the reader.
func (s *Session) scheduleRejoin() {
	s.rejoinAsking = false

	if s.rejoinTimer != nil {
		s.rejoinTimer.Stop()
	}

	s.rejoinTimer = time.AfterFunc(rejoinPoll, func() {
		select {
		case s.rejoinRetry <- struct{}{}:
		case <-s.done:
		default:
		}
	})
}

// sameObjects reports whether two object lists name the same objects, in any
// order.
func sameObjects(before, after []string) bool {
	if len(before) != len(after) {
		return false
	}

	a, b := slices.Clone(before), slices.Clone(after)
	slices.Sort(a)
	slices.Sort(b)

	return slices.Equal(a, b)
}
