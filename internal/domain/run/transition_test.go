package run

import (
	"reflect"
	"testing"

	"github.com/micanipho/aptify/internal/domain"
)

func wantIllegal(t *testing.T, err error) {
	t.Helper()
	if code, _ := domain.CodeOf(err); code != domain.CodeIllegalTransition {
		t.Fatalf("err = %v, want %s", err, domain.CodeIllegalTransition)
	}
}

func TestHappyPathFold(t *testing.T) {
	r := newTestRun(t, DefaultBudget())
	r.add(
		StepStarted{Step: StepPlan, Attempt: 1},
		CostRecorded{Cents: 2, InputTokens: 100, OutputTokens: 50, Model: "m"},
		StepSucceeded{Step: StepPlan, Attempt: 1, SpecificationID: "spec_1"},
		StepStarted{Step: StepGenerate, Attempt: 1},
		StepSucceeded{Step: StepGenerate, Attempt: 1, ArtifactSetID: "as_1"},
		StepStarted{Step: StepValidate, Attempt: 1},
		ValidationPassed{},
		StepSucceeded{Step: StepValidate, Attempt: 1},
		StepStarted{Step: StepPackage, Attempt: 1},
		StepSucceeded{Step: StepPackage, Attempt: 1, ArtifactSetID: "as_1"},
		RunSucceeded{ArtifactSetID: "as_1"},
	)
	s := r.state
	if s.Status != StatusSucceeded || s.Kind != KindGeneration {
		t.Fatalf("status %s kind %s", s.Status, s.Kind)
	}
	if !reflect.DeepEqual(s.Completed, []Step{StepPlan, StepGenerate, StepValidate, StepPackage}) {
		t.Fatalf("completed = %v", s.Completed)
	}
	if s.SpecificationID != "spec_1" || s.ArtifactSetID != "as_1" {
		t.Fatalf("spec %q artifact %q", s.SpecificationID, s.ArtifactSetID)
	}
	if s.Cost != (Cost{Cents: 2, InputTokens: 100, OutputTokens: 50, Calls: 1}) {
		t.Fatalf("cost = %+v", s.Cost)
	}
	if s.StartedAt != t0+1 || s.EndedAt != t0+12 || s.Version != 12 {
		t.Fatalf("startedAt %d endedAt %d version %d", s.StartedAt, s.EndedAt, s.Version)
	}
}

func TestTerminalRunAcceptsOnlyLog(t *testing.T) {
	r := newTestRun(t, DefaultBudget()).add(RunFailed{Reason: "x"})
	for _, e := range allEvents {
		if e.Type() == TypeLog {
			continue
		}
		wantIllegal(t, r.try(e))
	}
	before := r.state.Version
	r.add(Log{Level: LogInfo, Message: "late line"})
	if r.state.Version != before || r.state.Status != StatusFailed {
		t.Fatalf("log changed version or status: %+v", r.state)
	}
}

func TestLogDoesNotAdvanceVersion(t *testing.T) {
	r := newTestRun(t, DefaultBudget())
	v := r.state.Version
	r.add(Log{Level: LogDebug, Message: "hi"})
	if r.state.Version != v {
		t.Fatalf("version %d, want %d", r.state.Version, v)
	}
	if r.state.LastSeq != 2 {
		t.Fatalf("lastSeq %d, want 2", r.state.LastSeq)
	}
}

func TestIllegalStatusEdges(t *testing.T) {
	cases := []struct {
		name  string
		setup []Event
		bad   Event
	}{
		{"queued to validating", nil, StepStarted{Step: StepValidate, Attempt: 1}},
		{"planning to packaging", []Event{StepStarted{Step: StepPlan, Attempt: 1}}, StepStarted{Step: StepPackage, Attempt: 1}},
		{"planning to succeeded", []Event{StepStarted{Step: StepPlan, Attempt: 1}}, RunSucceeded{ArtifactSetID: "as_1"}},
		{"cancelling to planning", []Event{RunCancelRequested{}}, StepStarted{Step: StepPlan, Attempt: 1}},
		{"queued to cancelled", nil, RunCancelled{}},
		{"unknown step", nil, StepStarted{Step: "dance", Attempt: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newTestRun(t, DefaultBudget()).add(c.setup...)
			wantIllegal(t, r.try(c.bad))
		})
	}
}

func TestCanTransitionTerminalAndSame(t *testing.T) {
	for _, s := range []Status{StatusSucceeded, StatusFailed, StatusCancelled} {
		if CanTransition(s, s) || CanTransition(s, StatusQueued) {
			t.Errorf("terminal %s must not transition", s)
		}
	}
	if !CanTransition(StatusGenerating, StatusGenerating) {
		t.Error("same status must be allowed for a live run")
	}
}

func TestLogShapeIsEnforced(t *testing.T) {
	s := NewState(testRunID, DefaultBudget())

	_, err := Apply(s, StoredEvent{RunID: testRunID, Seq: 1, At: t0, Event: StepStarted{Step: StepPlan, Attempt: 1}})
	wantIllegal(t, err) // first event must be run.queued

	s, err = Apply(s, StoredEvent{RunID: testRunID, Seq: 1, At: t0, Event: RunQueued{Kind: KindGeneration}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Apply(s, StoredEvent{RunID: testRunID, Seq: 2, At: t0, Event: RunQueued{Kind: KindGeneration}})
	wantIllegal(t, err) // run.queued only once

	_, err = Apply(s, StoredEvent{RunID: testRunID, Seq: 3, At: t0, Event: Log{Level: LogInfo}})
	wantIllegal(t, err) // gap

	_, err = Apply(s, StoredEvent{RunID: testRunID, Seq: 1, At: t0, Event: Log{Level: LogInfo}})
	wantIllegal(t, err) // repeat

	_, err = Apply(s, StoredEvent{RunID: "run_other", Seq: 2, At: t0, Event: Log{Level: LogInfo}})
	wantIllegal(t, err) // wrong run
}

func TestApplyDoesNotMutateInput(t *testing.T) {
	r := newTestRun(t, DefaultBudget()).add(
		StepStarted{Step: StepPlan, Attempt: 1},
		StepSucceeded{Step: StepPlan, Attempt: 1},
		StepStarted{Step: StepGenerate, Attempt: 1},
		StepSucceeded{Step: StepGenerate, Attempt: 1},
		StepStarted{Step: StepValidate, Attempt: 1},
		ValidationFailed{Failures: someFailures},
	)
	before := r.state.clone()
	if _, err := Apply(r.state, r.next(StepStarted{Step: StepRepair, Attempt: 1})); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.state, before) {
		t.Fatalf("input state was mutated:\n got %+v\nwant %+v", r.state, before)
	}
}

func TestValidationAndRepairBookkeeping(t *testing.T) {
	r := newTestRun(t, DefaultBudget()).add(
		StepStarted{Step: StepPlan, Attempt: 1},
		StepSucceeded{Step: StepPlan, Attempt: 1},
		StepStarted{Step: StepGenerate, Attempt: 1},
		StepSucceeded{Step: StepGenerate, Attempt: 1},
		StepStarted{Step: StepValidate, Attempt: 1},
		ValidationFailed{Failures: someFailures},
	)
	if r.state.HasCompleted(StepValidate) || len(r.state.ValidationFailures) != 1 {
		t.Fatalf("after validation.failed: %+v", r.state)
	}

	r.add(StepStarted{Step: StepRepair, Attempt: 1})
	if r.state.RepairAttempts != 1 || len(r.state.ValidationFailures) != 1 {
		t.Fatalf("repair start must count the attempt and keep failures until success: %+v", r.state)
	}

	r.add(StepSucceeded{Step: StepRepair, Attempt: 1, ArtifactSetID: "as_2"})
	if len(r.state.ValidationFailures) != 0 {
		t.Fatal("a successful repair must consume its failures")
	}
	if r.state.ArtifactSetID != "as_2" {
		t.Fatalf("artifact = %q", r.state.ArtifactSetID)
	}

	r.add(StepStarted{Step: StepValidate, Attempt: 2}, ValidationFailed{Failures: someFailures})
	r.add(StepStarted{Step: StepValidate, Attempt: 3})
	if len(r.state.ValidationFailures) != 0 {
		t.Fatal("validate start must clear old failures")
	}
}

func TestRepairStartReopensValidateAndPackage(t *testing.T) {
	r := newTestRun(t, DefaultBudget())
	r.state.Completed = []Step{StepPlan, StepGenerate, StepValidate, StepPackage}
	r.state.Status = StatusValidating
	r.add(StepStarted{Step: StepRepair, Attempt: 1})
	if !reflect.DeepEqual(r.state.Completed, []Step{StepPlan, StepGenerate}) {
		t.Fatalf("completed = %v", r.state.Completed)
	}
}

func TestReplayEqualsIncrementalFold(t *testing.T) {
	r := newTestRun(t, DefaultBudget())
	r.drive(func(attempt int) bool { return attempt >= 2 })
	got, err := Replay(testRunID, DefaultBudget(), r.events)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, r.state) {
		t.Fatalf("replay differs:\n got %+v\nwant %+v", got, r.state)
	}
}
