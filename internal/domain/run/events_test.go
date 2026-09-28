package run

import (
	"encoding/json"
	"reflect"
	"testing"
)

// allEvents has one populated value per event type. The length check below fails when a new
// event type is added without a round-trip case.
var allEvents = []Event{
	RunQueued{Kind: KindRefinement, SpecificationID: "spec_1"},
	StepStarted{Step: StepPlan, Attempt: 1},
	StepSucceeded{Step: StepGenerate, Attempt: 2, ArtifactSetID: "as_1", SpecificationID: "spec_1", TemplateVersion: "1.0.0"},
	StepFailed{Step: StepRepair, Attempt: 1, Message: "boom", Retryable: true},
	ValidationPassed{},
	ValidationFailed{Failures: []ValidationFailure{{Gate: GateLint, File: "a.ts", Line: 4, Message: "no-unused-vars"}, {Gate: GateInstall, Message: "npm error E404"}}},
	CostRecorded{Cents: 12, InputTokens: 1000, OutputTokens: 200, Model: "anthropic/x", CacheReadTokens: 50, CacheWriteTokens: 10, SandboxMs: 0, Failed: true},
	RunCancelRequested{},
	RunCancelled{},
	RunFailed{Reason: "plan failed: timeout"},
	RunSucceeded{ArtifactSetID: "as_2"},
	Log{Level: LogWarn, Message: "discarded template file package-lock.json"},
}

func TestEveryEventTypeRoundTrips(t *testing.T) {
	if len(allEvents) != len(decoders) {
		t.Fatalf("allEvents has %d cases, decoders has %d types", len(allEvents), len(decoders))
	}
	for _, e := range allEvents {
		data, err := MarshalEvent(e)
		if err != nil {
			t.Fatalf("marshal %s: %v", e.Type(), err)
		}
		got, err := UnmarshalEvent(data)
		if err != nil {
			t.Fatalf("unmarshal %s (%s): %v", e.Type(), data, err)
		}
		if !reflect.DeepEqual(got, e) {
			t.Errorf("%s round-trip:\n got %#v\nwant %#v", e.Type(), got, e)
		}
	}
}

func TestEventJSONIsFlatWithTypeTag(t *testing.T) {
	cases := map[string]Event{
		`{"type":"step.started","step":"plan","attempt":1}`: StepStarted{Step: StepPlan, Attempt: 1},
		`{"type":"validation.passed"}`:                      ValidationPassed{},
		`{"type":"run.queued","kind":"generation"}`:         RunQueued{Kind: KindGeneration},
	}
	for want, e := range cases {
		got, err := MarshalEvent(e)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

func TestUnmarshalEventRejectsUnknownAndMalformed(t *testing.T) {
	for _, in := range []string{`{"type":"step.exploded"}`, `{}`, `not json`, `{"type":"step.started","attempt":"one"}`} {
		if e, err := UnmarshalEvent([]byte(in)); err == nil {
			t.Errorf("UnmarshalEvent(%s) = %#v, want an error", in, e)
		}
	}
}

func TestUnmarshalEventToleratesAddedFields(t *testing.T) {
	got, err := UnmarshalEvent([]byte(`{"type":"step.started","step":"plan","attempt":1,"addedLater":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != (StepStarted{Step: StepPlan, Attempt: 1}) {
		t.Fatalf("got %#v", got)
	}
}

func TestStoredEventRoundTrips(t *testing.T) {
	in := StoredEvent{RunID: "run_1", Seq: 7, At: t0, Event: CostRecorded{Cents: 3, Model: "sandbox", SandboxMs: 1200}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"runId":"run_1","seq":7,"at":1790000000000,"event":{"type":"cost.recorded","cents":3,"inputTokens":0,"outputTokens":0,"model":"sandbox","sandboxMs":1200}}`
	if string(data) != want {
		t.Fatalf("got  %s\nwant %s", data, want)
	}
	var out StoredEvent
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("got %#v, want %#v", out, in)
	}
}
