// Package run is the run state machine: an append-only event log, a fold that rebuilds state
// from it, and a policy that decides the next action from state alone. It performs no I/O.
package run

// Kind is what a run does.
type Kind string

const (
	KindGeneration Kind = "generation"
	KindRefinement Kind = "refinement"
)

// Status is where a run is. It changes only in the fold (transition.go).
type Status string

const (
	StatusQueued     Status = "queued"
	StatusPlanning   Status = "planning"
	StatusGenerating Status = "generating"
	StatusValidating Status = "validating"
	StatusRepairing  Status = "repairing"
	StatusPackaging  Status = "packaging"
	StatusPublishing Status = "publishing"
	StatusDeploying  Status = "deploying"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusCancelling Status = "cancelling"
	StatusCancelled  Status = "cancelled"
)

// Terminal reports whether the run can never change again (except by log lines).
func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCancelled
}

// Step is a unit of work the engine performs.
type Step string

const (
	StepPlan     Step = "plan"
	StepGenerate Step = "generate"
	StepValidate Step = "validate"
	StepRepair   Step = "repair"
	StepPackage  Step = "package"
	StepPublish  Step = "publish"
	StepDeploy   Step = "deploy"
)

// Pipeline is the step order for both run kinds. A refinement re-plans too, against the
// previous specification, so the specification never goes stale. Repair is not in the
// pipeline; the policy schedules it when validation fails.
var Pipeline = []Step{StepPlan, StepGenerate, StepValidate, StepPackage}

var stepStatus = map[Step]Status{
	StepPlan:     StatusPlanning,
	StepGenerate: StatusGenerating,
	StepValidate: StatusValidating,
	StepRepair:   StatusRepairing,
	StepPackage:  StatusPackaging,
	StepPublish:  StatusPublishing,
	StepDeploy:   StatusDeploying,
}

// StatusFor returns the status a step puts the run in, and false for an unknown step.
func StatusFor(step Step) (Status, bool) {
	s, ok := stepStatus[step]
	return s, ok
}

var legalEdges = map[Status][]Status{
	StatusQueued:     {StatusPlanning, StatusGenerating, StatusCancelling, StatusFailed},
	StatusPlanning:   {StatusGenerating, StatusCancelling, StatusFailed},
	StatusGenerating: {StatusValidating, StatusCancelling, StatusFailed},
	StatusValidating: {StatusRepairing, StatusPackaging, StatusPublishing, StatusCancelling, StatusFailed, StatusSucceeded},
	StatusRepairing:  {StatusGenerating, StatusValidating, StatusCancelling, StatusFailed},
	StatusPackaging:  {StatusPublishing, StatusSucceeded, StatusCancelling, StatusFailed},
	StatusPublishing: {StatusDeploying, StatusSucceeded, StatusCancelling, StatusFailed},
	StatusDeploying:  {StatusSucceeded, StatusCancelling, StatusFailed},
	StatusCancelling: {StatusCancelled, StatusFailed},
}

// CanTransition reports whether a run may move from one status to another. Staying in the
// same status is always allowed for a live run; a terminal run may not move at all.
func CanTransition(from, to Status) bool {
	if from.Terminal() {
		return false
	}
	if from == to {
		return true
	}
	for _, s := range legalEdges[from] {
		if s == to {
			return true
		}
	}
	return false
}
