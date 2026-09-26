package run

import "fmt"

// ActionKind is what the policy tells the engine to do next.
type ActionKind string

const (
	// ActionDone: the run is terminal; do nothing.
	ActionDone ActionKind = "done"
	// ActionFinish: append the terminal event for Outcome.
	ActionFinish ActionKind = "finish"
	// ActionStep: run Step at Attempt.
	ActionStep ActionKind = "step"
)

// Outcome is how a finished run ends.
type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	OutcomeCancelled Outcome = "cancelled"
)

// Reason prefixes for policy failures. Attribution classifies failures by matching them;
// change them here and there together.
const (
	ReasonCostCeiling      = "Cost ceiling reached"
	ReasonWallClock        = "Wall-clock budget exceeded."
	ReasonRepairsExhausted = "Validation still failing after"
)

// Action is the policy's decision.
type Action struct {
	Kind    ActionKind
	Outcome Outcome // ActionFinish only
	Reason  string  // ActionFinish with OutcomeFailed only
	Step    Step    // ActionStep only
	Attempt int     // ActionStep only
}

// NextAction decides what happens next from state and the current time alone. Budgets are
// checked before any step is dispatched, never after. It panics on a run that has not been
// queued, which is a programming error in the caller.
func NextAction(s State, nowMs int64) Action {
	if s.LastSeq == 0 {
		panic("run: NextAction called on a run with no events")
	}

	// 1. Terminal.
	if s.Status.Terminal() {
		return Action{Kind: ActionDone}
	}
	// 2. Cancellation wins over everything else.
	if s.CancelRequested {
		return Action{Kind: ActionFinish, Outcome: OutcomeCancelled}
	}
	// 3. Cost ceiling.
	if s.Cost.Cents >= s.Budget.MaxCostCents {
		return fail(fmt.Sprintf("%s (%d/%d cents).", ReasonCostCeiling, s.Cost.Cents, s.Budget.MaxCostCents))
	}
	// 4. Wall clock.
	if nowMs-s.StartedAt >= s.Budget.MaxWallClockMs {
		return fail(ReasonWallClock)
	}
	// 5. Validation failures: resume an interrupted repair, else repair or give up. A repair
	// that was started but never succeeded leaves the run in repairing with its failures
	// still present; it resumes at the same attempt, before the budget check, because that
	// attempt was already counted.
	if len(s.ValidationFailures) > 0 {
		if s.Status == StatusRepairing {
			return step(StepRepair, s.RepairAttempts)
		}
		if s.RepairAttempts >= s.Budget.MaxRepairAttempts {
			return fail(fmt.Sprintf("%s %d repair attempts.", ReasonRepairsExhausted, s.RepairAttempts))
		}
		return step(StepRepair, s.RepairAttempts+1)
	}
	// 6. A repair just succeeded: validate again.
	if s.Status == StatusRepairing {
		return step(StepValidate, s.Attempts[StepValidate]+1)
	}
	// 7. The first pipeline step not yet completed. If the run is already sitting in that
	// step's status it was interrupted mid-flight: resume the same attempt.
	for _, st := range Pipeline {
		if s.HasCompleted(st) {
			continue
		}
		if status, _ := StatusFor(st); s.Status == status && s.Attempts[st] > 0 {
			return step(st, s.Attempts[st])
		}
		return step(st, s.Attempts[st]+1)
	}
	// 8. Nothing pending. In practice package emits run.succeeded first.
	return Action{Kind: ActionFinish, Outcome: OutcomeSucceeded}
}

func step(st Step, attempt int) Action {
	return Action{Kind: ActionStep, Step: st, Attempt: attempt}
}

func fail(reason string) Action {
	return Action{Kind: ActionFinish, Outcome: OutcomeFailed, Reason: reason}
}
