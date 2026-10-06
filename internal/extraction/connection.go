package extraction

import (
	"context"

	"github.com/go-rod/rod/lib/cdp"
	"github.com/ysmood/goob"
)

// connection shares Rod's transport while each request gets its own Rod state cache.
// It only broadcasts immutable library events; protocol encoding and I/O stay in Rod.
type connection struct {
	client *cdp.Client
	events *goob.Observable
	done   chan struct{}
}

func newConnection(ctx context.Context, url string) (*connection, error) {
	client, e := cdp.StartWithURL(ctx, url, nil)
	if e != nil {
		return nil, e
	}
	eventCtx, cancel := context.WithCancel(ctx)
	c := &connection{client: client, events: goob.New(eventCtx), done: make(chan struct{})}
	go func() {
		defer close(c.done)
		defer cancel()
		for event := range client.Event() {
			c.events.Publish(event)
		}
	}()
	return c, nil
}
func (c *connection) forContext(ctx context.Context) *eventClient {
	return &eventClient{Client: c.client, ctx: ctx, events: c.events.Subscribe(ctx)}
}

type eventClient struct {
	*cdp.Client
	ctx    context.Context
	events goob.Events
}

func (c *eventClient) Event() <-chan *cdp.Event {
	out := make(chan *cdp.Event)
	go func() {
		defer close(out)
		for {
			select {
			case <-c.ctx.Done():
				return
			case value, ok := <-c.events:
				if !ok {
					return
				}
				select {
				case <-c.ctx.Done():
					return
				case out <- value.(*cdp.Event):
				}
			}
		}
	}()
	return out
}
