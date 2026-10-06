package extraction

import (
	"context"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/cdp"
	"github.com/ysmood/goob"
)

func TestEventFanoutAndCancellation(t *testing.T) {
	life, stop := context.WithCancel(context.Background())
	defer stop()
	conn := &connection{client: cdp.New(), events: goob.New(life)}
	ctx, cancel := context.WithCancel(life)
	first, second := conn.forContext(ctx).Event(), conn.forContext(ctx).Event()
	event := &cdp.Event{Method: "Test.event", SessionID: "session"}
	conn.events.Publish(event)
	for _, stream := range []<-chan *cdp.Event{first, second} {
		select {
		case got := <-stream:
			if got != event {
				t.Fatal("event changed")
			}
		case <-time.After(time.Second):
			t.Fatal("event not broadcast")
		}
	}
	cancel()
	for _, stream := range []<-chan *cdp.Event{first, second} {
		select {
		case _, ok := <-stream:
			if ok {
				t.Fatal("cancelled event stream open")
			}
		case <-time.After(time.Second):
			t.Fatal("event stream leaked")
		}
	}
	deadline := time.Now().Add(time.Second)
	for conn.events.Len() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if conn.events.Len() != 0 {
		t.Fatal("subscriber leaked")
	}
}
