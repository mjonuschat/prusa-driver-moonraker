package moonrakersim_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func withSpoolman(rate, active int) moonrakersim.Options {
	return moonrakersim.Options{Spoolman: &moonrakersim.Spoolman{
		SyncRate: rate, Active: active, Spools: []moonrakersim.Spool{moonrakersim.ABSSpool(108)},
	}}
}

type configAnswer struct {
	Config map[string]struct {
		SyncRate int `json:"sync_rate"`
	} `json:"config"`
}

func proxy(id int) map[string]any {
	return map[string]any{"request_method": "GET", "path": "/v1/spool/" + strconv.Itoa(id)}
}

func usedWeight(t *testing.T, r *rig) float64 {
	t.Helper()

	var body struct {
		Used float64 `json:"used_weight"`
	}

	r.ok("server.spoolman.proxy", proxy(108), &body)

	return body.Used
}

func TestWithoutASpoolmanTheConfigHasNoSpoolmanSection(t *testing.T) {
	r := start(t, moonrakersim.Options{})

	var got configAnswer

	r.ok("server.config", nil, &got)

	if _, held := got.Config["spoolman"]; held {
		t.Fatalf("config = %+v", got.Config)
	}

	if reply := r.call("server.spoolman.get_spool_id", nil); reply.Error == nil {
		t.Fatal("get_spool_id answered on a machine with no Spoolman")
	}
}

func TestWithASpoolmanTheConfigCarriesTheSyncRate(t *testing.T) {
	for rate, want := range map[int]int{7: 7, 0: 5} {
		r := start(t, withSpoolman(rate, 0))

		var got configAnswer

		r.ok("server.config", nil, &got)

		if got.Config["spoolman"].SyncRate != want {
			t.Fatalf("rate %d: config = %+v", rate, got.Config)
		}
	}
}

func TestTheActiveSpoolAndItsProxyAnswerAreServed(t *testing.T) {
	r := start(t, withSpoolman(0, 108))

	var active struct {
		ID int `json:"spool_id"`
	}

	r.ok("server.spoolman.get_spool_id", nil, &active)

	if active.ID != 108 {
		t.Fatalf("spool_id = %d", active.ID)
	}

	var spool struct {
		Filament struct{ Material string } `json:"filament"`
	}

	r.ok("server.spoolman.proxy", proxy(108), &spool)

	if spool.Filament.Material != "ABS" {
		t.Fatalf("spool = %+v", spool)
	}

	var status struct {
		Connected bool `json:"spoolman_connected"`
		ID        int  `json:"spool_id"`
	}

	r.ok("server.spoolman.status", nil, &status)

	if !status.Connected || status.ID != 108 {
		t.Fatalf("status = %+v", status)
	}
}

func TestAnUnknownSpoolIsRefused(t *testing.T) {
	r := start(t, withSpoolman(0, 0))

	if reply := r.call("server.spoolman.proxy", proxy(999)); reply.Error == nil {
		t.Fatal("an unknown spool was answered")
	}
}

func TestSetActiveSpoolPushesTheNotification(t *testing.T) {
	r := start(t, withSpoolman(0, 0))

	r.host.SetActiveSpool(108)

	got := r.read()
	if got.Method != "notify_active_spool_set" || string(got.Params) != `[{"spool_id":108}]` {
		t.Fatalf("frame = %s %s", got.Method, got.Params)
	}

	r.host.SetActiveSpool(0)

	got = r.read()
	if string(got.Params) != `[{"spool_id":null}]` {
		t.Fatalf("params = %s", got.Params)
	}
}

func TestQueuedUsageLandsAfterOneSyncRate(t *testing.T) {
	r := start(t, withSpoolman(1, 108))

	before := usedWeight(t, r)

	r.host.QueueUsage(108, 10)

	if got := usedWeight(t, r); got != before {
		t.Fatalf("used_weight moved at once: %v", got)
	}

	time.Sleep(1300 * time.Millisecond)

	if got := usedWeight(t, r); got != before+10 {
		t.Fatalf("used_weight = %v, want %v", got, before+10)
	}
}

func TestRefuseAndAllowChangeAnAnswer(t *testing.T) {
	r := start(t, withSpoolman(0, 108))

	r.host.Refuse("server.spoolman.proxy", "spoolman is down")

	if reply := r.call("server.spoolman.proxy", proxy(108)); reply.Error == nil || reply.Error.Message != "spoolman is down" {
		t.Fatalf("reply = %+v", reply)
	}

	r.host.Allow("server.spoolman.proxy")

	if reply := r.call("server.spoolman.proxy", proxy(108)); reply.Error != nil {
		t.Fatalf("still refused: %s", reply.Error.Message)
	}
}

func TestCallTimesRecordsEachCall(t *testing.T) {
	r := start(t, withSpoolman(0, 108))

	r.ok("server.spoolman.get_spool_id", nil, nil)
	r.ok("server.spoolman.get_spool_id", nil, nil)

	times := r.host.CallTimes("server.spoolman.get_spool_id")
	if len(times) != 2 || times[1].Before(times[0]) {
		t.Fatalf("times = %v", times)
	}

	if len(r.host.CallTimes("server.spoolman.status")) != 0 {
		t.Fatal("a call that was not made has a time")
	}
}

func TestASilencedMethodIsReadAndNeverAnswered(t *testing.T) {
	r := start(t, withSpoolman(0, 108))

	r.host.Silence("server.spoolman.get_spool_id")

	if err := r.conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "server.spoolman.get_spool_id"}); err != nil {
		t.Fatal(err)
	}

	_ = r.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))

	var out reply
	if err := r.conn.ReadJSON(&out); err == nil {
		t.Fatalf("a silenced method was answered: %+v", out)
	}

	if len(r.host.CallTimes("server.spoolman.get_spool_id")) != 1 {
		t.Fatal("the silenced call was not recorded")
	}
}

func TestARemovedMethodAnswersMethodNotFound(t *testing.T) {
	r := start(t, withSpoolman(0, 108))

	r.host.Remove("server.spoolman.get_spool_id")

	if reply := r.call("server.spoolman.get_spool_id", nil); reply.Error == nil {
		t.Fatal("a removed method was answered")
	}
}
