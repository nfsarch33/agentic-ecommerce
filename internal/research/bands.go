package research

import "fmt"

// BandKind is one of the spec 2.1 recordings: the wholesale band (with its
// MOQ), the retail band, or the demand signal.
type BandKind string

const (
	BandWholesale BandKind = "wholesale"
	BandRetail    BandKind = "retail"
	BandDemand    BandKind = "demand"
)

// BandRecord is one search's answer: the band's low and high (or the
// signal's value in LowCents), the MOQ it applies to, and the source url
// the number came from. A number without a source url is a defect in the
// run, never a band.
type BandRecord struct {
	Kind      BandKind
	LowCents  int64
	HighCents int64
	MOQUnits  int64
	SourceURL string
	Query     string
}

// Established is a band a search returned, carrying its numbers, MOQ,
// source and the query that found it.
type Established struct {
	LowCents  int64
	HighCents int64
	MOQUnits  int64
	SourceURL string
	Query     string
}

// BandResult is the recording's state: the established bands (first answer
// per kind wins), the search count, and - when a band is still missing
// after exactly four searches - the not-established stop naming it.
type BandResult struct {
	Wholesale      *Established
	Retail         *Established
	Demand         *Established
	Searches       int
	Done           bool
	NotEstablished string
	Defect         string
}

// Establish folds the recordings into the spec 2.1 state: every number
// must carry a source url; both bands present is established; a band still
// missing after four searches stops the scenario as not established.
func Establish(records []BandRecord) BandResult {
	res := BandResult{Searches: len(records)}
	for _, r := range records {
		if r.SourceURL == "" {
			res.Defect = fmt.Sprintf("%s number without a source url is a defect in the run", r.Kind)
			return res
		}
	}
	for _, r := range records {
		e := &Established{LowCents: r.LowCents, HighCents: r.HighCents, MOQUnits: r.MOQUnits, SourceURL: r.SourceURL, Query: r.Query}
		switch r.Kind {
		case BandWholesale:
			if res.Wholesale == nil {
				res.Wholesale = e
			}
		case BandRetail:
			if res.Retail == nil {
				res.Retail = e
			}
		case BandDemand:
			if res.Demand == nil {
				res.Demand = e
			}
		}
	}
	if res.Wholesale != nil && res.Retail != nil {
		res.Done = true
		return res
	}
	if len(records) >= 4 {
		missing := "retail"
		switch {
		case res.Wholesale == nil && res.Retail == nil:
			missing = "wholesale and retail"
		case res.Wholesale == nil:
			missing = "wholesale"
		}
		res.NotEstablished = fmt.Sprintf("%s band not established after %d searches", missing, len(records))
	}
	return res
}
