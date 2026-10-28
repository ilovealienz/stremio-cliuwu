package player

import (
	"testing"

	"stremio-cliuwu/internal/store"
)

func TestUpNextSlot(t *testing.T) {
	p := New(store.Config{})
	p.now = &PlayRequest{VideoID: "ep1"}

	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "ep2", URL: "auto"}, "ep1"); !ok || !p.Queued().Auto {
		t.Fatal("auto pick should fill an empty slot")
	}

	p.Queue(PlayRequest{VideoID: "ep2", URL: "mine", Auto: true})
	if q := p.Queued(); q.URL != "mine" || q.Auto {
		t.Fatalf("n should replace the auto pick, got %+v", q)
	}
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "ep2", URL: "auto"}, "ep1"); ok || p.Queued().URL != "mine" {
		t.Fatal("auto pick replaced a hand-queued stream")
	}

	p.Unqueue()
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "ep2", URL: "auto"}, "ep1"); ok || p.Queued() != nil {
		t.Fatal("auto pick refilled a slot cleared by hand")
	}

	p.noAuto = false // a new episode started
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "ep3", URL: "auto"}, "ep0"); ok {
		t.Fatal("auto pick for an episode that is no longer playing")
	}
	if p.State().NextLabel != "" {
		t.Fatal("label set without a queued stream")
	}
}

func TestUpNextAfterEnd(t *testing.T) {
	p := New(store.Config{})
	var started []PlayRequest
	p.start = func(req PlayRequest) { started = append(started, req) }
	p.now = &PlayRequest{VideoID: "ep1"}
	p.ended = true

	ok, now := p.QueueAuto(PlayRequest{VideoID: "ep2", URL: "auto"}, "ep1")
	if !ok || !now || len(started) != 1 || !started[0].Auto || p.Queued() != nil {
		t.Fatalf("ok=%v started=%v plays=%v queued=%v", ok, now, started, p.Queued())
	}
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "ep2", URL: "again"}, "ep1"); ok && len(started) != 1 {
		t.Fatal("second pick started another play")
	}
}

func TestClearAuto(t *testing.T) {
	p := New(store.Config{})
	p.now = &PlayRequest{VideoID: "ep1"}
	p.QueueAuto(PlayRequest{VideoID: "ep2"}, "ep1")
	p.ClearAuto()
	if p.Queued() != nil || p.State().NextLabel != "" {
		t.Fatal("auto pick not cleared")
	}
	p.Queue(PlayRequest{VideoID: "ep2", Label: "mine"})
	p.ClearAuto()
	if p.Queued() == nil {
		t.Fatal("ClearAuto removed a hand-queued stream")
	}
}

func TestQueueAfterEndPlaysAtOnce(t *testing.T) {
	p := New(store.Config{})
	var started []PlayRequest
	p.start = func(req PlayRequest) { started = append(started, req) }
	p.now = &PlayRequest{VideoID: "ep1"}

	if p.Queue(PlayRequest{VideoID: "ep2"}) || len(started) != 0 || p.Queued() == nil {
		t.Fatal("while playing, n should queue")
	}
	p.Unqueue()
	p.ended = true
	if !p.Queue(PlayRequest{VideoID: "ep2"}) || len(started) != 1 || p.Queued() != nil {
		t.Fatalf("after the end, n should play at once: started=%v queued=%v", started, p.Queued())
	}
}
