// Package auditjoin is the nightly join of counting-proxy writes to approval
// rows (v18900-5 acceptance 3). The MVP-1 exit reads its verdict: a planted
// unapproved write must surface as exactly one unmatched row, a clean day is
// 0, and an EMPTY proxy log is NOT_RUN — never a clean 0, which would let a
// dead proxy masquerade as a compliant day.
package auditjoin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// ApprovalsReader lists the approval row ids that were APPROVED in a window.
type ApprovalsReader interface {
	ApprovedIDs(ctx context.Context, since time.Time) (map[string]struct{}, error)
}

// Status of a join run.
type Status string

const (
	StatusOK        Status = "OK"
	StatusUnmatched Status = "UNMATCHED"
	StatusNotRun    Status = "NOT_RUN"
)

// Result is the join verdict.
type Result struct {
	Status    Status
	Reads     int
	Writes    int
	Unmatched []string
	Err       error
}

// Join reads the proxy audit log and matches every WRITE row's approval id
// against the approved rows. Reads (GETs) are counted but not matched.
func Join(ctx context.Context, logPath string, approvals ApprovalsReader, since time.Time) Result {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Status: StatusNotRun, Err: fmt.Errorf("no proxy log at %s", logPath)}
		}
		return Result{Status: StatusNotRun, Err: err}
	}
	defer f.Close()

	approved, err := approvals.ApprovedIDs(ctx, since)
	if err != nil {
		return Result{Status: StatusNotRun, Err: fmt.Errorf("reading approvals: %w", err)}
	}

	var res Result
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	haveRow := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		row, err := parseRow(line)
		if err != nil {
			return Result{Status: StatusNotRun, Err: fmt.Errorf("bad audit line %q: %w", string(line), err)}
		}
		haveRow = true
		if row.Method == "GET" || row.Method == "HEAD" {
			res.Reads++
			continue
		}
		res.Writes++
		if row.ApprovalID == "" {
			res.Unmatched = append(res.Unmatched, row.Method+" "+row.Path+" (no approval id)")
			continue
		}
		if _, ok := approved[row.ApprovalID]; !ok {
			res.Unmatched = append(res.Unmatched, row.Method+" "+row.Path+" approval="+row.ApprovalID)
		}
	}
	if err := sc.Err(); err != nil {
		return Result{Status: StatusNotRun, Err: err}
	}
	if !haveRow {
		// An empty log means the proxy served nothing — that is NOT a
		// clean day, and reporting 0 would hide a dead proxy.
		return Result{Status: StatusNotRun, Err: errors.New("proxy log is empty")}
	}
	if len(res.Unmatched) > 0 {
		res.Status = StatusUnmatched
		return res
	}
	res.Status = StatusOK
	return res
}

type row struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	ApprovalID string `json:"approval_id"`
}

func parseRow(line []byte) (row, error) {
	var r row
	if err := json.Unmarshal(line, &r); err != nil {
		return r, err
	}
	if r.Method == "" || r.Path == "" {
		return r, fmt.Errorf("method/path missing")
	}
	return r, nil
}
