package run

import "github.com/micanipho/aptify/internal/domain"

// Budget bounds a run. Every budget is checked before work is dispatched.
type Budget struct {
	MaxRepairAttempts int   `json:"maxRepairAttempts"`
	MaxCostCents      int64 `json:"maxCostCents"`
	MaxWallClockMs    int64 `json:"maxWallClockMs"`
}

// DefaultBudget is 3 repairs, 200 cents and 15 minutes of wall clock.
func DefaultBudget() Budget {
	return Budget{MaxRepairAttempts: 3, MaxCostCents: 200, MaxWallClockMs: 15 * 60 * 1000}
}

// Cost is the running total of priced usage.
type Cost struct {
	Cents        int64 `json:"cents"`
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	Calls        int   `json:"calls"`
}

// State is the projection of a run's event log. It is never stored as truth; it is always
// rebuilt by Replay.
type State struct {
	ID                 domain.RunID
	Kind               Kind
	Status             Status
	SpecificationID    domain.SpecificationID
	ArtifactSetID      domain.ArtifactSetID
	Attempts           map[Step]int // latest attempt number per step
	Completed          []Step
	RepairAttempts     int
	ValidationFailures []ValidationFailure
	Budget             Budget
	Cost               Cost
	CancelRequested    bool
	FailureReason      string
	StartedAt          int64 // epoch ms of run.queued
	EndedAt            int64 // epoch ms of the terminal event; 0 while running
	LastSeq            int64 // seq of the last folded event, including log lines
	Version            int   // +1 per non-log event
}

// NewState is a run before its first event.
func NewState(id domain.RunID, budget Budget) State {
	return State{ID: id, Status: StatusQueued, Budget: budget, Attempts: map[Step]int{}}
}

// HasCompleted reports whether a step is in the completed set.
func (s State) HasCompleted(step Step) bool {
	for _, c := range s.Completed {
		if c == step {
			return true
		}
	}
	return false
}

func (s State) clone() State {
	c := s
	c.Attempts = make(map[Step]int, len(s.Attempts))
	for k, v := range s.Attempts {
		c.Attempts[k] = v
	}
	c.Completed = append([]Step(nil), s.Completed...)
	c.ValidationFailures = append([]ValidationFailure(nil), s.ValidationFailures...)
	return c
}
