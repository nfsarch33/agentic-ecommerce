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
	return join(ctx, logPath, approvals, since, "")
}

// JoinApproval is Join scoped to the writes one approval authorised: the
// audit verdict for a single listing step. Only rows carrying approvalID
// count. A step that wrote but left no proxy row under its approval is
// NOT_RUN — the write bypassed the proxy or the proxy is dead, and neither
// is a clean step.
func JoinApproval(ctx context.Context, logPath string, approvals ApprovalsReader, since time.Time, approvalID string) Result {
	if approvalID == "" {
		return Result{Status: StatusNotRun, Err: errors.New("no approval id for this step")}
	}
	res := join(ctx, logPath, approvals, since, approvalID)
	if res.Status != StatusNotRun && res.Writes == 0 {
		return Result{Status: StatusNotRun, Reads: res.Reads, Err: fmt.Errorf("no proxy write row for approval %s", approvalID)}
	}
	return res
}

func join(ctx context.Context, logPath string, approvals ApprovalsReader, since time.Time, onlyApproval string) Result {
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
		if onlyApproval != "" && row.ApprovalID != onlyApproval {
			continue
		}
		res.tally(row, approved)
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

// tally counts one proxy row into the result.
func (res *Result) tally(r row, approved map[string]struct{}) {
	switch {
	case r.Kind == "intent":
		// Write-ahead: exactly one intent row per write ATTEMPT is the
		// counted write; done/unknown outcome rows are not counted again.
		res.Writes++
		if r.ApprovalID == "" {
			res.Unmatched = append(res.Unmatched, r.Method+" "+r.Path+" (no approval id)")
			return
		}
		res.match(r, approved)
	case r.Kind == "" && r.Method != "GET" && r.Method != "HEAD":
		// Legacy pre-intent rows (kind absent): still writes.
		res.Writes++
		res.match(r, approved)
	case r.Method == "GET" || r.Method == "HEAD":
		res.Reads++
	}
	// Outcome rows (done/unknown) count as neither reads nor writes: the
	// intent row already carried the attempt.
}

func (res *Result) match(r row, approved map[string]struct{}) {
	if _, ok := approved[r.ApprovalID]; !ok {
		res.Unmatched = append(res.Unmatched, r.Method+" "+r.Path+" approval="+r.ApprovalID)
	}
}

type row struct {
	Kind       string `json:"kind"`
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
