package run

import (
	"strconv"
	"testing"

	"github.com/micanipho/aptify/internal/domain"
)

const (
	testRunID domain.RunID = "run_test"
	t0        int64        = 1_790_000_000_000
)

// testRun is an in-memory log that folds as it goes, failing the test on any illegal event.
type testRun struct {
	t      *testing.T
	events []StoredEvent
	state  State
}

func newTestRun(t *testing.T, budget Budget) *testRun {
	t.Helper()
	r := &testRun{t: t, state: NewState(testRunID, budget)}
	r.add(RunQueued{Kind: KindGeneration})
	return r
}

func (r *testRun) next(e Event) StoredEvent {
	seq := r.state.LastSeq + 1
	return StoredEvent{RunID: testRunID, Seq: seq, At: t0 + seq, Event: e}
}

// try folds e without failing the test, keeping it only if it was legal.
func (r *testRun) try(e Event) error {
	se := r.next(e)
	s, err := Apply(r.state, se)
	if err != nil {
		return err
	}
	r.events = append(r.events, se)
	r.state = s
	return nil
}

func (r *testRun) add(events ...Event) *testRun {
	r.t.Helper()
	for _, e := range events {
		if err := r.try(e); err != nil {
			r.t.Fatalf("apply %s: %v", e.Type(), err)
		}
	}
	return r
}

var someFailures = []ValidationFailure{
	{Gate: GateTypecheck, File: "app/page.tsx", Line: 3, Message: "TS2304: Cannot find name 'x'."},
}

// perform plays the engine for one action, appending the events a real step would emit.
// validatePasses decides the outcome of each validation, by attempt.
func (r *testRun) perform(a Action, validatePasses func(attempt int) bool) {
	r.t.Helper()
	switch a.Kind {
	case ActionDone:
	case ActionFinish:
		switch a.Outcome {
		case OutcomeCancelled:
			r.add(RunCancelled{})
		case OutcomeFailed:
			r.add(RunFailed{Reason: a.Reason})
		case OutcomeSucceeded:
			r.add(RunSucceeded{ArtifactSetID: r.state.ArtifactSetID})
		}
	case ActionStep:
		r.add(StepStarted{Step: a.Step, Attempt: a.Attempt})
		switch a.Step {
		case StepPlan:
			r.add(CostRecorded{Cents: 1, Model: "anthropic/plan"},
				StepSucceeded{Step: a.Step, Attempt: a.Attempt, SpecificationID: "spec_1"})
		case StepGenerate, StepRepair:
			r.add(CostRecorded{Cents: 1, Model: "anthropic/code"},
				StepSucceeded{Step: a.Step, Attempt: a.Attempt, ArtifactSetID: "as_1", TemplateVersion: "1.0.0"})
		case StepValidate:
			r.add(CostRecorded{Model: "sandbox", SandboxMs: 1000})
			if validatePasses(a.Attempt) {
				r.add(ValidationPassed{}, StepSucceeded{Step: a.Step, Attempt: a.Attempt})
			} else {
				r.add(ValidationFailed{Failures: someFailures})
			}
		case StepPackage:
			r.add(StepSucceeded{Step: a.Step, Attempt: a.Attempt, ArtifactSetID: r.state.ArtifactSetID},
				RunSucceeded{ArtifactSetID: r.state.ArtifactSetID})
		case StepPublish, StepDeploy:
			r.t.Fatalf("step %s is not implemented", a.Step)
		}
	}
}

// drive asks the policy and performs its answer until the run is done, returning each step
// dispatched as "step#attempt" and the final finish action (zero if none).
func (r *testRun) drive(validatePasses func(attempt int) bool) (steps []string, finish Action) {
	r.t.Helper()
	for i := 0; i < 40; i++ {
		a := NextAction(r.state, t0+1000)
		switch a.Kind {
		case ActionDone:
			return steps, finish
		case ActionFinish:
			finish = a
		case ActionStep:
			steps = append(steps, string(a.Step)+"#"+strconv.Itoa(a.Attempt))
		}
		r.perform(a, validatePasses)
	}
	r.t.Fatal("run did not finish within 40 actions")
	return nil, Action{}
}
