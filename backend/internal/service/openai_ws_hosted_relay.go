package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	openaiwsv2 "github.com/MACOS-DO/sub4api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

type openAIWSHostedRelayEvent struct {
	generation uint64
	kind       coderws.MessageType
	payload    []byte
	err        error
}

// A passthrough session owns one physical socket at a time. The relay keeps
// only this session's complete history and switches sockets before adding a
// hosted capability. Obsolete reader results can never terminate the new one.
type openAIWSHostedRelayConn struct {
	mu              sync.Mutex
	writeMu         sync.Mutex
	conn            openaiwsv2.FrameConn
	generation      uint64
	events          chan openAIWSHostedRelayEvent
	stop            chan struct{}
	closeOnce       sync.Once
	redial          func(context.Context) (openaiwsv2.FrameConn, error)
	reject          func(error)
	initial         openAIWSHostedTools
	initialized     bool
	lastResponseID  string
	lastWindowID    string
	lastInput       []json.RawMessage
	lastComplete    bool
	currentInput    []json.RawMessage
	currentComplete bool
	currentWindowID string
	collector       openAIWSToolCallReplayCollector
}

func newOpenAIWSHostedRelayConn(conn openaiwsv2.FrameConn, initial openAIWSHostedTools, initialized bool, redial func(context.Context) (openaiwsv2.FrameConn, error), reject func(error)) *openAIWSHostedRelayConn {
	c := &openAIWSHostedRelayConn{conn: conn, generation: 1, events: make(chan openAIWSHostedRelayEvent, 1), stop: make(chan struct{}), redial: redial, reject: reject, initial: initial, initialized: initialized}
	go c.pump(conn, 1)
	return c
}
func (c *openAIWSHostedRelayConn) pump(conn openaiwsv2.FrameConn, generation uint64) {
	for {
		kind, payload, err := conn.ReadFrame(context.Background())
		select {
		case c.events <- openAIWSHostedRelayEvent{generation, kind, payload, err}:
		case <-c.stop:
			return
		}
		if err != nil {
			return
		}
	}
}
func (c *openAIWSHostedRelayConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	for {
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case <-c.stop:
			return 0, nil, errOpenAIWSConnClosed
		case event := <-c.events:
			c.mu.Lock()
			if event.generation != c.generation {
				c.mu.Unlock()
				continue
			}
			if event.err == nil {
				kind, id, _ := parseOpenAIWSEventEnvelope(event.payload)
				if c.currentComplete {
					c.collector.AddEvent(kind, event.payload)
				}
				if kind == "response.completed" || kind == "response.done" {
					c.lastResponseID = id
					c.lastWindowID = c.currentWindowID
					c.lastInput = combineOpenAIWSReplayItems(c.currentInput, c.collector.AllItems())
					c.lastComplete = c.currentComplete
					size := 0
					for _, item := range c.lastInput {
						size += len(item)
					}
					if size > 8<<20 {
						c.lastInput = nil
						c.lastComplete = false
					}
				} else if isOpenAIWSTerminalEvent(kind) {
					c.lastComplete = false
					c.lastInput = nil
				}
			}
			c.mu.Unlock()
			return event.kind, event.payload, event.err
		}
	}
}
func (c *openAIWSHostedRelayConn) WriteFrame(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	conn := c.conn
	responseCreate := gjson.GetBytes(payload, "type").String() == "response.create"
	if !responseCreate {
		c.mu.Unlock()
		return conn.WriteFrame(ctx, kind, payload)
	}
	required := openAIWSHostedToolsDeclared(payload)
	previous := openAIWSPayloadStringFromRaw(payload, "previous_response_id")
	window := openAIWSPayloadCodexWindowID(payload)
	complete := previous == "" || (c.lastComplete && previous == c.lastResponseID && (window == "" || c.lastWindowID == "" || window == c.lastWindowID))
	var prior []json.RawMessage
	if previous != "" && complete {
		prior = c.lastInput
	}
	input, inputExists, inputErr := buildOpenAIWSReplayInputSequence(prior, prior != nil, payload, previous != "")
	complete = complete && inputExists && inputErr == nil && len(payload) <= 8<<20
	missing := c.initialized && required&^c.initial != 0
	c.mu.Unlock()
	if missing {
		if previous != "" {
			if !complete {
				failure := openAIWSHostedHistoryUnavailable()
				if c.reject != nil {
					c.reject(failure)
				}
				return failure
			}
			full, safe, err := buildOpenAIWSCurrentTurnRetryPayload(payload, input, true, openAIWSPayloadStringFromRaw(payload, "model"))
			if err != nil {
				return err
			}
			if !safe {
				failure := openAIWSHostedHistoryUnavailable()
				if c.reject != nil {
					c.reject(failure)
				}
				return failure
			}
			payload = full
		}
		replacement, err := c.redial(ctx)
		if err != nil {
			return err
		}
		c.mu.Lock()
		select {
		case <-c.stop:
			c.mu.Unlock()
			_ = replacement.Close()
			return errOpenAIWSConnClosed
		default:
		}
		c.generation++
		generation := c.generation
		c.conn = replacement
		c.initialized = false
		c.initial = 0
		c.mu.Unlock()
		go c.pump(replacement, generation)
		_ = conn.Close()
		conn = replacement
	}
	c.mu.Lock()
	c.currentInput, c.currentComplete, c.currentWindowID = input, complete, window
	c.collector = openAIWSToolCallReplayCollector{}
	c.mu.Unlock()
	if err := conn.WriteFrame(ctx, kind, payload); err != nil {
		return err
	}
	c.mu.Lock()
	if !c.initialized {
		c.initialized = true
		c.initial = required
	}
	c.mu.Unlock()
	return nil
}
func (c *openAIWSHostedRelayConn) Close() error {
	var err error
	c.closeOnce.Do(func() { close(c.stop); c.mu.Lock(); conn := c.conn; c.mu.Unlock(); err = conn.Close() })
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
