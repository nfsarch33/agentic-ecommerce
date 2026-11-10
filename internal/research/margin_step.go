package research

import (
	"fmt"

	"github.com/nfsarch33/agentic-ecommerce/internal/margingate"
)

// StepKind is the research-and-margin-gate step's verdict.
type StepKind string

const (
	StepNotEstablished StepKind = "not established"
	StepGateReject     StepKind = "gate reject"
	StepPass           StepKind = "pass"
)

// RecordRow is one band's numbers - the low, the high, the MOQ - each
// with its source url - the run record's shape.
type RecordRow struct {
	LowCents  int64
	HighCents int64
	MOQUnits  int64
	SourceURL string
	Kind      BandKind
}

// StepResult is the step's answer: the verdict kind, the run record (each
// band's low, high and MOQ with its source), the gate's verdict line, and
// the stop line on the not-established path.
type StepResult struct {
	Kind    StepKind
	Record  []RecordRow
	Verdict string
	Stop    string
}

// Evaluate runs research-then-gate: establish the bands from the search
// recordings (a missing band after four searches stops the scenario before
// any draft, model or store call), then apply the deterministic margin
// gate. The step is a pure function of its recordings and its gate inputs
// - no model, store or draft call exists to make, and the returned kind is
// the whole proof. On pass, the run record carries every band's low, high
// and MOQ with its source url, and the gate's verdict.
func Evaluate(records []BandRecord, gateInputs margingate.Inputs) StepResult {
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
		record = append(record, RecordRow{LowCents: bands.Wholesale.LowCents, HighCents: bands.Wholesale.HighCents, MOQUnits: bands.Wholesale.MOQUnits, SourceURL: bands.Wholesale.SourceURL, Kind: BandWholesale})
	}
	if bands.Retail != nil {
		record = append(record, RecordRow{LowCents: bands.Retail.LowCents, HighCents: bands.Retail.HighCents, MOQUnits: bands.Retail.MOQUnits, SourceURL: bands.Retail.SourceURL, Kind: BandRetail})
	}
	if bands.Demand != nil {
		record = append(record, RecordRow{LowCents: bands.Demand.LowCents, HighCents: bands.Demand.HighCents, MOQUnits: bands.Demand.MOQUnits, SourceURL: bands.Demand.SourceURL, Kind: BandDemand})
	}
	return StepResult{Kind: StepPass, Record: record, Verdict: v.Reason}
}
