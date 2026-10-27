package ipc

import (
	"encoding/json"
	"time"
)

const ProtocolVersion = 1
const MaxFrameBytes = 16 << 20 // 16 MiB

type EnvelopeType string

const (
	TypeRequest  EnvelopeType = "request"
	TypeResponse EnvelopeType = "response"
	TypeEvent    EnvelopeType = "event"
)

type Request struct {
	Type      EnvelopeType    `json:"type"`
	ID        string          `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	TimeoutMs int             `json:"timeoutMs,omitempty"`
}

type Response struct {
	Type   EnvelopeType `json:"type"`
	ID     string       `json:"id"`
	OK     bool         `json:"ok"`
	Result any          `json:"result,omitempty"`
	Error  *ErrorBody   `json:"error,omitempty"`
}

type ErrorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

type Event struct {
	Type          EnvelopeType `json:"type"`
	Event         string       `json:"event"`
	CorrelationID string       `json:"correlationId,omitempty"`
	TS            time.Time    `json:"ts"`
	Payload       any          `json:"payload,omitempty"`
	// Top-level fields used by engine.ready for convenience
	ProtocolVersion int    `json:"protocolVersion,omitempty"`
	EngineVersion   string `json:"engineVersion,omitempty"`
}

func NewResponse(id string, result any) Response {
	return Response{Type: TypeResponse, ID: id, OK: true, Result: result}
}

func NewErrorResponse(id string, code, message string, retryable bool) Response {
	return Response{
		Type: TypeResponse,
		ID:   id,
		OK:   false,
		Error: &ErrorBody{
			Code:      code,
			Message:   message,
			Retryable: retryable,
		},
	}
}

func NewEvent(name string, payload any) Event {
	return Event{
		Type:    TypeEvent,
		Event:   name,
		TS:      time.Now().UTC(),
		Payload: payload,
	}
}
