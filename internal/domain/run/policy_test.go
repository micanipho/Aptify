package run

import (
	"reflect"
	"testing"
)

func passes(from int) func(int) bool { return func(attempt int) bool { return attempt >= from } }

func never(int) bool { return false }

func TestDriveHappyPath(t *testing.T) {
	r := newTestRun(t, DefaultBudget())
	steps, _ := r.drive(passes(1))
	want := []string{"plan#1", "generate#1", "validate#1", "package#1"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
	if r.state.Status != StatusSucceeded {
		t.Fatalf("status = %s", r.state.Status)
	}
}

func TestDriveRepairThenPass(t *testing.T) {
	r := newTestRun(t, DefaultBudget())
	steps, _ := r.drive(passes(3))
	want := []string{"plan#1", "generate#1", "validate#1", "repair#1", "validate#2", "repair#2", "validate#3", "package#1"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
	if r.state.Status != StatusSucceeded {
		t.Fatalf("status = %s", r.state.Status)
	}
}

func TestDriveRepairsExhausted(t *testing.T) {
	r := newTestRun(t, DefaultBudget())
	steps, finish := r.drive(never)
	want := []string{"plan#1", "generate#1", "validate#1", "repair#1", "validate#2", "repair#2", "validate#3", "repair#3", "validate#4"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
	if finish.Reason != "Validation still failing after 3 repair attempts." {
		t.Fatalf("reason = %q", finish.Reason)
	}
	if r.state.Status != StatusFailed || r.state.FailureReason != finish.Reason {
		t.Fatalf("state = %+v", r.state)
	}
}

func TestInterruptedStepResumesSameAttempt(t *testing.T) {
	cases := []struct {
		name  string
		setup []Event
		want  Action
	}{
		{
			"plan",
			[]Event{StepStarted{Step: StepPlan, Attempt: 1}},
			step(StepPlan, 1),
		},
		{
			"generate after a cost was recorded",
			[]Event{
				StepStarted{Step: StepPlan, Attempt: 1}, StepSucceeded{Step: StepPlan, Attempt: 1},
				StepStarted{Step: StepGenerate, Attempt: 1}, CostRecorded{Cents: 5, Model: "m"},
			},
			step(StepGenerate, 1),
		},
		{
			"validate after a repair",
			[]Event{
				StepStarted{Step: StepPlan, Attempt: 1}, StepSucceeded{Step: StepPlan, Attempt: 1},
				StepStarted{Step: StepGenerate, Attempt: 1}, StepSucceeded{Step: StepGenerate, Attempt: 1},
				StepStarted{Step: StepValidate, Attempt: 1}, ValidationFailed{Failures: someFailures},
				StepStarted{Step: StepRepair, Attempt: 1}, StepSucceeded{Step: StepRepair, Attempt: 1},
				StepStarted{Step: StepValidate, Attempt: 2},
			},
			step(StepValidate, 2),
		},
		{
			"repair",
			[]Event{
				StepStarted{Step: StepPlan, Attempt: 1}, StepSucceeded{Step: StepPlan, Attempt: 1},
				StepStarted{Step: StepGenerate, Attempt: 1}, StepSucceeded{Step: StepGenerate, Attempt: 1},
				StepStarted{Step: StepValidate, Attempt: 1}, ValidationFailed{Failures: someFailures},
				StepStarted{Step: StepRepair, Attempt: 1},
			},
			step(StepRepair, 1),
		},
		{
			"package",
			[]Event{
				StepStarted{Step: StepPlan, Attempt: 1}, StepSucceeded{Step: StepPlan, Attempt: 1},
				StepStarted{Step: StepGenerate, Attempt: 1}, StepSucceeded{Step: StepGenerate, Attempt: 1},
				StepStarted{Step: StepValidate, Attempt: 1}, ValidationPassed{}, StepSucceeded{Step: StepValidate, Attempt: 1},
				StepStarted{Step: StepPackage, Attempt: 1},
			},
			step(StepPackage, 1),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newTestRun(t, DefaultBudget()).add(c.setup...)
			if got := NextAction(r.state, t0+1000); got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestInterruptedFinalRepairResumesInsteadOfFailing(t *testing.T) {
	b := DefaultBudget()
	b.MaxRepairAttempts = 1
	r := newTestRun(t, b).add(
		StepStarted{Step: StepPlan, Attempt: 1}, StepSucceeded{Step: StepPlan, Attempt: 1},
		StepStarted{Step: StepGenerate, Attempt: 1}, StepSucceeded{Step: StepGenerate, Attempt: 1},
		StepStarted{Step: StepValidate, Attempt: 1}, ValidationFailed{Failures: someFailures},
		StepStarted{Step: StepRepair, Attempt: 1},
	)
	if got := NextAction(r.state, t0+1000); got != step(StepRepair, 1) {
		t.Fatalf("got %+v, want repair#1 resumed", got)
	}
}

func TestSuccessfulRepairIsFollowedByValidateNotRepair(t *testing.T) {
	r := newTestRun(t, DefaultBudget()).add(
		StepStarted{Step: StepPlan, Attempt: 1}, StepSucceeded{Step: StepPlan, Attempt: 1},
		StepStarted{Step: StepGenerate, Attempt: 1}, StepSucceeded{Step: StepGenerate, Attempt: 1},
		StepStarted{Step: StepValidate, Attempt: 1}, ValidationFailed{Failures: someFailures},
		StepStarted{Step: StepRepair, Attempt: 1}, StepSucceeded{Step: StepRepair, Attempt: 1},
	)
	if got := NextAction(r.state, t0+1000); got != step(StepValidate, 2) {
		t.Fatalf("got %+v, want validate#2", got)
	}
}

func TestCostCeiling(t *testing.T) {
	r := newTestRun(t, DefaultBudget()).add(
		StepStarted{Step: StepPlan, Attempt: 1},
		CostRecorded{Cents: 199, Model: "m"},
	)
	if got := NextAction(r.state, t0+1000); got.Kind != ActionStep {
		t.Fatalf("under the ceiling: got %+v", got)
	}
	r.add(CostRecorded{Cents: 1, Model: "m", Failed: true})
	got := NextAction(r.state, t0+1000)
	if got != fail("Cost ceiling reached (200/200 cents).") {
		t.Fatalf("got %+v", got)
	}
}

func TestWallClock(t *testing.T) {
	r := newTestRun(t, DefaultBudget())
	start := r.state.StartedAt
	if got := NextAction(r.state, start+DefaultBudget().MaxWallClockMs-1); got.Kind != ActionStep {
		t.Fatalf("1ms before the limit: got %+v", got)
	}
	if got := NextAction(r.state, start+DefaultBudget().MaxWallClockMs); got != fail(ReasonWallClock) {
		t.Fatalf("at the limit: got %+v", got)
	}
}

func TestCancellationWinsOverBudgets(t *testing.T) {
	r := newTestRun(t, DefaultBudget()).add(
		StepStarted{Step: StepPlan, Attempt: 1},
		CostRecorded{Cents: 500, Model: "m"},
		RunCancelRequested{},
	)
	got := NextAction(r.state, r.state.StartedAt+DefaultBudget().MaxWallClockMs*2)
	if got != (Action{Kind: ActionFinish, Outcome: OutcomeCancelled}) {
		t.Fatalf("got %+v", got)
	}
	r.perform(got, never)
	if r.state.Status != StatusCancelled || NextAction(r.state, t0).Kind != ActionDone {
		t.Fatalf("state = %+v", r.state)
	}
}

func TestBudgetFailureFoldsFromEveryLiveStatus(t *testing.T) {
	r := newTestRun(t, DefaultBudget()).add(
		StepStarted{Step: StepPlan, Attempt: 1},
		CostRecorded{Cents: 200, Model: "m"},
	)
	r.perform(NextAction(r.state, t0+1000), never)
	if r.state.Status != StatusFailed {
		t.Fatalf("status = %s", r.state.Status)
	}
}

func TestNextActionPanicsOnUnqueuedRun(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	NextAction(NewState(testRunID, DefaultBudget()), t0)
}
