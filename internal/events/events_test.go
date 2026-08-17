package events

import (
	"context"
	"sync"
	"testing"
	"time"
)

type collectingSink struct {
	events []Event
}

type blockingSink struct {
	mu             sync.Mutex
	events         []Event
	firstStarted   chan struct{}
	secondReceived chan struct{}
	release        chan struct{}
}

func (sink *blockingSink) Publish(event Event) {
	if event.Sequence == 1 {
		close(sink.firstStarted)
		<-sink.release
	}
	sink.mu.Lock()
	sink.events = append(sink.events, event)
	if event.Sequence == 2 {
		close(sink.secondReceived)
	}
	sink.mu.Unlock()
}

func TestStreamDeliversConcurrentEventsInSequenceOrder(t *testing.T) {
	sink := &blockingSink{firstStarted: make(chan struct{}), secondReceived: make(chan struct{}), release: make(chan struct{})}
	stream := NewStream(sink)
	ctx := WithSink(context.Background(), stream)
	go Emit(ctx, Event{EventType: CheckStarted, CheckID: "first"})
	select {
	case <-sink.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first event was not delivered")
	}
	go Emit(ctx, Event{EventType: CheckStarted, CheckID: "second"})
	select {
	case <-sink.secondReceived:
		t.Fatal("second event overtook blocked first delivery")
	case <-time.After(25 * time.Millisecond):
	}
	close(sink.release)
	select {
	case <-sink.secondReceived:
	case <-time.After(time.Second):
		t.Fatal("second event was not delivered")
	}
	if len(sink.events) != 2 || sink.events[0].Sequence != 1 || sink.events[1].Sequence != 2 {
		t.Fatalf("events were not delivered in sequence order: %#v", sink.events)
	}
}

func TestAsyncSinkNeverDropsLifecycleEvents(t *testing.T) {
	sink := &collectingSink{}
	async := NewAsyncSink(sink, 1)
	for index := 0; index < 100; index++ {
		async.Publish(Event{Sequence: uint64(index + 1), EventType: CheckFinished, CheckID: "check"})
		async.Publish(Event{EventType: ProcessOutput, CheckID: "check", Message: "noisy output"})
	}
	async.Close()
	lifecycle := 0
	for _, event := range sink.events {
		if event.EventType == CheckFinished {
			lifecycle++
		}
	}
	if lifecycle != 100 {
		t.Fatalf("lifecycle events were dropped: got %d", lifecycle)
	}
}

func (sink *collectingSink) Publish(event Event) {
	sink.events = append(sink.events, event)
}

func TestStreamAssignsSharedMonotonicSequence(t *testing.T) {
	first := &collectingSink{}
	second := &collectingSink{}
	stream := NewStream(first, second)
	ctx := WithMetadata(WithSink(context.Background(), stream), "run-1", "project-1")
	Emit(ctx, Event{EventType: CheckStarted, CheckID: "build"})
	Emit(ctx, Event{EventType: CheckFinished, CheckID: "build", Status: "PASS"})

	if len(first.events) != 2 || len(second.events) != 2 {
		t.Fatalf("expected both sinks to receive two events: %#v %#v", first.events, second.events)
	}
	if first.events[0].Sequence != 1 || first.events[1].Sequence != 2 {
		t.Fatalf("unexpected sequence: %#v", first.events)
	}
	if second.events[0].Sequence != first.events[0].Sequence || second.events[1].Sequence != first.events[1].Sequence {
		t.Fatalf("sinks did not receive shared sequence values: %#v %#v", first.events, second.events)
	}
	if first.events[0].RunID != "run-1" || first.events[0].ProjectID != "project-1" {
		t.Fatalf("context metadata was not applied: %#v", first.events[0])
	}
}
