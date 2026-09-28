package run

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/micanipho/aptify/internal/domain"
)

// Events are persisted and replayed. Adding a field is safe; changing the meaning of an
// existing field is not.

// EventType is the "type" tag of an event's JSON form.
type EventType string

const (
	TypeRunQueued          EventType = "run.queued"
	TypeStepStarted        EventType = "step.started"
	TypeStepSucceeded      EventType = "step.succeeded"
	TypeStepFailed         EventType = "step.failed"
	TypeValidationPassed   EventType = "validation.passed"
	TypeValidationFailed   EventType = "validation.failed"
	TypeCostRecorded       EventType = "cost.recorded"
	TypeRunCancelRequested EventType = "run.cancelRequested"
	TypeRunCancelled       EventType = "run.cancelled"
	TypeRunFailed          EventType = "run.failed"
	TypeRunSucceeded       EventType = "run.succeeded"
	TypeLog                EventType = "log"
)

// Event is one entry in a run's log. The set is closed: every implementation is in this file.
//
//sumtype:decl
type Event interface {
	Type() EventType
	isEvent()
}

type RunQueued struct {
	Kind            Kind                   `json:"kind"`
	SpecificationID domain.SpecificationID `json:"specificationId,omitempty"`
}

type StepStarted struct {
	Step    Step `json:"step"`
	Attempt int  `json:"attempt"`
}

type StepSucceeded struct {
	Step            Step                   `json:"step"`
	Attempt         int                    `json:"attempt"`
	ArtifactSetID   domain.ArtifactSetID   `json:"artifactSetId,omitempty"`
	SpecificationID domain.SpecificationID `json:"specificationId,omitempty"`
	TemplateVersion string                 `json:"templateVersion,omitempty"`
}

type StepFailed struct {
	Step      Step   `json:"step"`
	Attempt   int    `json:"attempt"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type ValidationPassed struct{}

type ValidationFailed struct {
	Failures []ValidationFailure `json:"failures"`
}

// CostRecorded is priced usage for one model call or one sandbox validation. Failed marks
// the cost of a call that errored after spending.
type CostRecorded struct {
	Cents            int64  `json:"cents"`
	InputTokens      int64  `json:"inputTokens"`
	OutputTokens     int64  `json:"outputTokens"`
	Model            string `json:"model"`
	CacheReadTokens  int64  `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64  `json:"cacheWriteTokens,omitempty"`
	SandboxMs        int64  `json:"sandboxMs,omitempty"`
	Failed           bool   `json:"failed,omitempty"`
}

type RunCancelRequested struct{}

type RunCancelled struct{}

type RunFailed struct {
	Reason string `json:"reason"`
}

type RunSucceeded struct {
	ArtifactSetID domain.ArtifactSetID `json:"artifactSetId"`
}

// Log is a progress line. It never changes state and does not advance the version.
type Log struct {
	Level   LogLevel `json:"level"`
	Message string   `json:"message"`
}

type LogLevel string

const (
	LogDebug LogLevel = "debug"
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
)

// Gate is one stage of sandbox validation.
type Gate string

const (
	GateInstall   Gate = "install"
	GateTypecheck Gate = "typecheck"
	GateLint      Gate = "lint"
	GateTest      Gate = "test"
	GateBuild     Gate = "build"
)

// ValidationFailure is one structured failure from a validation gate.
type ValidationFailure struct {
	Gate    Gate   `json:"gate"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

func (RunQueued) Type() EventType          { return TypeRunQueued }
func (StepStarted) Type() EventType        { return TypeStepStarted }
func (StepSucceeded) Type() EventType      { return TypeStepSucceeded }
func (StepFailed) Type() EventType         { return TypeStepFailed }
func (ValidationPassed) Type() EventType   { return TypeValidationPassed }
func (ValidationFailed) Type() EventType   { return TypeValidationFailed }
func (CostRecorded) Type() EventType       { return TypeCostRecorded }
func (RunCancelRequested) Type() EventType { return TypeRunCancelRequested }
func (RunCancelled) Type() EventType       { return TypeRunCancelled }
func (RunFailed) Type() EventType          { return TypeRunFailed }
func (RunSucceeded) Type() EventType       { return TypeRunSucceeded }
func (Log) Type() EventType                { return TypeLog }

func (RunQueued) isEvent()          {}
func (StepStarted) isEvent()        {}
func (StepSucceeded) isEvent()      {}
func (StepFailed) isEvent()         {}
func (ValidationPassed) isEvent()   {}
func (ValidationFailed) isEvent()   {}
func (CostRecorded) isEvent()       {}
func (RunCancelRequested) isEvent() {}
func (RunCancelled) isEvent()       {}
func (RunFailed) isEvent()          {}
func (RunSucceeded) isEvent()       {}
func (Log) isEvent()                {}

func decodeAs[T Event](data []byte) (Event, error) {
	var v T
	err := json.Unmarshal(data, &v)
	return v, err
}

var decoders = map[EventType]func([]byte) (Event, error){
	TypeRunQueued:          decodeAs[RunQueued],
	TypeStepStarted:        decodeAs[StepStarted],
	TypeStepSucceeded:      decodeAs[StepSucceeded],
	TypeStepFailed:         decodeAs[StepFailed],
	TypeValidationPassed:   decodeAs[ValidationPassed],
	TypeValidationFailed:   decodeAs[ValidationFailed],
	TypeCostRecorded:       decodeAs[CostRecorded],
	TypeRunCancelRequested: decodeAs[RunCancelRequested],
	TypeRunCancelled:       decodeAs[RunCancelled],
	TypeRunFailed:          decodeAs[RunFailed],
	TypeRunSucceeded:       decodeAs[RunSucceeded],
	TypeLog:                decodeAs[Log],
}

// MarshalEvent encodes an event as a flat JSON object with a leading "type" tag, e.g.
// {"type":"step.started","step":"plan","attempt":1}.
func MarshalEvent(e Event) ([]byte, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	tag, err := json.Marshal(string(e.Type()))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(`{"type":`)
	buf.Write(tag)
	if inner := bytes.TrimSpace(body[1 : len(body)-1]); len(inner) > 0 {
		buf.WriteByte(',')
		buf.Write(inner)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalEvent decodes an event from its flat JSON form. An unknown type tag is an error:
// silently skipping an event would fold a different history than the one stored.
func UnmarshalEvent(data []byte) (Event, error) {
	var head struct {
		Type EventType `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("decode event: %w", err)
	}
	decode, ok := decoders[head.Type]
	if !ok {
		return nil, fmt.Errorf("decode event: unknown type %q", head.Type)
	}
	e, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode %s event: %w", head.Type, err)
	}
	return e, nil
}

// StoredEvent is an event as persisted: seq is 1-based and gapless per run, at is epoch ms.
type StoredEvent struct {
	RunID domain.RunID
	Seq   int64
	At    int64
	Event Event
}

type storedEventJSON struct {
	RunID domain.RunID    `json:"runId"`
	Seq   int64           `json:"seq"`
	At    int64           `json:"at"`
	Event json.RawMessage `json:"event"`
}

func (s StoredEvent) MarshalJSON() ([]byte, error) {
	ev, err := MarshalEvent(s.Event)
	if err != nil {
		return nil, err
	}
	return json.Marshal(storedEventJSON{RunID: s.RunID, Seq: s.Seq, At: s.At, Event: ev})
}

func (s *StoredEvent) UnmarshalJSON(data []byte) error {
	var raw storedEventJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	ev, err := UnmarshalEvent(raw.Event)
	if err != nil {
		return err
	}
	*s = StoredEvent{RunID: raw.RunID, Seq: raw.Seq, At: raw.At, Event: ev}
	return nil
}
