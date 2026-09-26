package run

import "github.com/micanipho/aptify/internal/domain"

// Replay folds a run's full log into its state. The log must start at seq 1 with run.queued
// and be gapless.
func Replay(id domain.RunID, budget Budget, events []StoredEvent) (State, error) {
	s := NewState(id, budget)
	for _, e := range events {
		var err error
		if s, err = Apply(s, e); err != nil {
			return State{}, err
		}
	}
	return s, nil
}

// Apply folds one stored event into the state and returns the new state; the input is never
// modified. It is the only place a run's status changes. An illegal event returns an
// illegal_transition error rather than being ignored.
func Apply(prev State, se StoredEvent) (State, error) {
	if se.RunID != prev.ID {
		return State{}, illegal("event for run %q applied to run %q", se.RunID, prev.ID)
	}
	if se.Seq != prev.LastSeq+1 {
		return State{}, illegal("expected seq %d, got %d", prev.LastSeq+1, se.Seq)
	}
	_, queued := se.Event.(RunQueued)
	if prev.LastSeq == 0 && !queued {
		return State{}, illegal("first event must be %s, got %s", TypeRunQueued, se.Event.Type())
	}
	if prev.LastSeq > 0 && queued {
		return State{}, illegal("%s may only be the first event", TypeRunQueued)
	}
	if _, isLog := se.Event.(Log); prev.Status.Terminal() && !isLog {
		return State{}, illegal("run is %s and accepts only log events, got %s", prev.Status, se.Event.Type())
	}

	s := prev.clone()
	s.LastSeq = se.Seq

	switch e := se.Event.(type) {
	case RunQueued:
		s.Kind = e.Kind
		s.SpecificationID = e.SpecificationID
		s.StartedAt = se.At

	case StepStarted:
		to, ok := StatusFor(e.Step)
		if !ok {
			return State{}, illegal("unknown step %q", e.Step)
		}
		if err := s.moveTo(to); err != nil {
			return State{}, err
		}
		s.Attempts[e.Step] = e.Attempt
		switch e.Step {
		case StepRepair:
			s.RepairAttempts = e.Attempt
			s.Completed = without(s.Completed, StepValidate, StepPackage)
		case StepValidate:
			s.ValidationFailures = nil
		case StepPlan, StepGenerate, StepPackage, StepPublish, StepDeploy:
		}

	case StepSucceeded:
		if !s.HasCompleted(e.Step) {
			s.Completed = append(s.Completed, e.Step)
		}
		if e.Step == StepRepair {
			// The repair consumed the failures; keeping them would schedule a second repair
			// for the same errors.
			s.ValidationFailures = nil
		}
		if e.ArtifactSetID != "" {
			s.ArtifactSetID = e.ArtifactSetID
		}
		if e.SpecificationID != "" {
			s.SpecificationID = e.SpecificationID
		}

	case StepFailed:

	case ValidationPassed:
		s.ValidationFailures = nil

	case ValidationFailed:
		s.ValidationFailures = append([]ValidationFailure(nil), e.Failures...)
		s.Completed = without(s.Completed, StepValidate)

	case CostRecorded:
		s.Cost.Cents += e.Cents
		s.Cost.InputTokens += e.InputTokens
		s.Cost.OutputTokens += e.OutputTokens
		s.Cost.Calls++

	case RunCancelRequested:
		if err := s.moveTo(StatusCancelling); err != nil {
			return State{}, err
		}
		s.CancelRequested = true

	case RunCancelled:
		if err := s.moveTo(StatusCancelled); err != nil {
			return State{}, err
		}
		s.EndedAt = se.At

	case RunFailed:
		if err := s.moveTo(StatusFailed); err != nil {
			return State{}, err
		}
		s.FailureReason = e.Reason
		s.EndedAt = se.At

	case RunSucceeded:
		if err := s.moveTo(StatusSucceeded); err != nil {
			return State{}, err
		}
		s.ArtifactSetID = e.ArtifactSetID
		s.EndedAt = se.At

	case Log:
		return s, nil // log lines do not advance the version

	default:
		return State{}, illegal("unknown event %T", se.Event)
	}

	s.Version++
	return s, nil
}

func (s *State) moveTo(to Status) error {
	if !CanTransition(s.Status, to) {
		return illegal("cannot move from %s to %s", s.Status, to)
	}
	s.Status = to
	return nil
}

func without(steps []Step, drop ...Step) []Step {
	out := steps[:0:0]
	for _, st := range steps {
		keep := true
		for _, d := range drop {
			if st == d {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, st)
		}
	}
	return out
}

func illegal(format string, args ...any) error {
	return domain.Errorf(domain.CodeIllegalTransition, format, args...)
}
