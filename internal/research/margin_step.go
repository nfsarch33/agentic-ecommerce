package research

import (
	"fmt"

	"github.com/nfsarch33/agentic-ecommerce/internal/margingate"
)

// StepCalls are the driver-side effects this step owns none of: the model
// caller, the store caller, the draft writer. They sit on this pure
// boundary only so the tests can prove the not-established stop and the
// gate reject call none of them - the step itself never calls.
type StepCalls struct {
	Model func()
	Store func()
	Draft func()
}

// StepKind is the research-and-margin-gate step's verdict.
type StepKind string

const (
	StepNotEstablished StepKind = "not established"
	StepGateReject     StepKind = "gate reject"
	StepPass           StepKind = "pass"
)

// RecordRow is one number with its source url - the run record's shape.
type RecordRow struct {
	Number    int64
	SourceURL string
	Kind      BandKind
}

// StepResult is the step's answer: the verdict kind, the run record (each
// number with its source), the gate's verdict line, and the stop line on
// the not-established path.
type StepResult struct {
	Kind    StepKind
	Record  []RecordRow
	Verdict string
	Stop    string
}

// Evaluate runs research-then-gate: establish the bands from the search
// recordings (a missing band after four searches stops the scenario before
// any draft, model or store call), then apply the deterministic margin
// gate. On pass, the run record carries every number with its source url
// and the gate's verdict.
func Evaluate(records []BandRecord, gateInputs margingate.Inputs, calls StepCalls) StepResult {
	bands := Establish(records)
	switch {
	case bands.Defect != "":
		return StepResult{Kind: StepNotEstablished, Stop: bands.Defect}
	case bands.NotEstablished != "":
		return StepResult{Kind: StepNotEstablished, Stop: bands.NotEstablished}
	case !bands.Done:
		return StepResult{Kind: StepNotEstablished, Stop: fmt.Sprintf("bands still establishing after %d searches", bands.Searches)}
	}
	v := margingate.Gate(gateInputs)
	if !v.Pass {
		return StepResult{Kind: StepGateReject, Verdict: v.Reason}
	}
	record := []RecordRow{}
	if bands.Wholesale != nil {
		record = append(record, RecordRow{Number: bands.Wholesale.LowCents, SourceURL: bands.Wholesale.SourceURL, Kind: BandWholesale})
	}
	if bands.Retail != nil {
		record = append(record, RecordRow{Number: bands.Retail.LowCents, SourceURL: bands.Retail.SourceURL, Kind: BandRetail})
	}
	if bands.Demand != nil {
		record = append(record, RecordRow{Number: bands.Demand.LowCents, SourceURL: bands.Demand.SourceURL, Kind: BandDemand})
	}
	return StepResult{Kind: StepPass, Record: record, Verdict: v.Reason}
}
