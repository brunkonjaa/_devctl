package events

import (
	"context"
	"sync"
	"time"
)

type Type string

const (
	VerificationStarted  Type = "verification_started"
	CheckStarted         Type = "check_started"
	CheckSkipped         Type = "check_skipped"
	ProcessStarted       Type = "process_started"
	ProcessOutput        Type = "process_output"
	ProcessFinished      Type = "process_finished"
	CheckFinished        Type = "check_finished"
	CheckEvidence        Type = "check_evidence"
	EvidenceWritten      Type = "evidence_written"
	VerificationFinished Type = "verification_finished"
	RepairLifecycle      Type = "repair_lifecycle"
)

type Event struct {
	Sequence   uint64    `json:"sequence"`
	Timestamp  time.Time `json:"timestamp"`
	RunID      string    `json:"run_id,omitempty"`
	ProjectID  string    `json:"project_id,omitempty"`
	CheckID    string    `json:"check_id,omitempty"`
	EventType  Type      `json:"event_type"`
	Status     string    `json:"status,omitempty"`
	ElapsedMS  int64     `json:"elapsed_ms,omitempty"`
	Message    string    `json:"message,omitempty"`
	Executable string    `json:"executable,omitempty"`
	Arguments  []string  `json:"arguments,omitempty"`
	Stream     string    `json:"stream,omitempty"`
}

type Sink interface {
	Publish(Event)
}

type Stream struct {
	mu          sync.Mutex
	sequence    uint64
	subscribers []Sink
}

func NewStream(subscribers ...Sink) *Stream {
	return &Stream{subscribers: append([]Sink(nil), subscribers...)}
}

func (stream *Stream) Publish(event Event) {
	stream.mu.Lock()
	stream.sequence++
	event.Sequence = stream.sequence
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	// Keep delivery under the same lock as sequence assignment. This makes
	// journal order match sequence order even when scheduler goroutines emit
	// concurrently.
	for _, subscriber := range stream.subscribers {
		publishSafely(subscriber, event)
	}
	stream.mu.Unlock()
}

func publishSafely(sink Sink, event Event) {
	defer func() { _ = recover() }()
	sink.Publish(event)
}

type AsyncSink struct {
	mu        sync.Mutex
	lifecycle []Event
	output    []Event
	wake      chan struct{}
	closed    bool
	wg        sync.WaitGroup
	sink      Sink
	maxOutput int
}

func NewAsyncSink(sink Sink, buffer int) *AsyncSink {
	if buffer < 1 {
		buffer = 1
	}
	async := &AsyncSink{wake: make(chan struct{}, 1), sink: sink, maxOutput: buffer}
	async.wg.Add(1)
	go func() {
		defer async.wg.Done()
		for {
			async.mu.Lock()
			var event Event
			hasEvent := false
			if len(async.lifecycle) > 0 {
				event = async.lifecycle[0]
				async.lifecycle = async.lifecycle[1:]
				hasEvent = true
			} else if len(async.output) > 0 {
				event = async.output[0]
				async.output = async.output[1:]
				hasEvent = true
			} else if async.closed {
				async.mu.Unlock()
				return
			}
			async.mu.Unlock()
			if hasEvent {
				publishSafely(async.sink, event)
				continue
			}
			<-async.wake
		}
	}()
	return async
}

func (async *AsyncSink) Publish(event Event) {
	async.mu.Lock()
	if async.closed {
		async.mu.Unlock()
		return
	}
	if event.EventType == ProcessOutput {
		if len(async.output) >= async.maxOutput {
			async.mu.Unlock()
			return
		}
		async.output = append(async.output, event)
	} else {
		// Lifecycle events are never dropped. Only transient process output is
		// bounded and allowed to be lost by a slow renderer.
		async.lifecycle = append(async.lifecycle, event)
	}
	async.mu.Unlock()
	async.signal()
}

func (async *AsyncSink) Close() {
	async.mu.Lock()
	async.closed = true
	async.mu.Unlock()
	async.signal()
	async.wg.Wait()
}

func (async *AsyncSink) signal() {
	select {
	case async.wake <- struct{}{}:
	default:
	}
}

type contextKey uint8

const (
	sinkKey contextKey = iota
	metadataKey
	checkKey
)

type metadata struct {
	runID     string
	projectID string
}

func WithSink(ctx context.Context, sink Sink) context.Context {
	return context.WithValue(ctx, sinkKey, sink)
}

func WithMetadata(ctx context.Context, runID, projectID string) context.Context {
	return context.WithValue(ctx, metadataKey, metadata{runID: runID, projectID: projectID})
}

func WithCheck(ctx context.Context, checkID string) context.Context {
	return context.WithValue(ctx, checkKey, checkID)
}

func Emit(ctx context.Context, event Event) {
	sink, ok := ctx.Value(sinkKey).(Sink)
	if !ok || sink == nil {
		return
	}
	if values, ok := ctx.Value(metadataKey).(metadata); ok {
		if event.RunID == "" {
			event.RunID = values.runID
		}
		if event.ProjectID == "" {
			event.ProjectID = values.projectID
		}
	}
	if event.CheckID == "" {
		if checkID, ok := ctx.Value(checkKey).(string); ok {
			event.CheckID = checkID
		}
	}
	sink.Publish(event)
}
