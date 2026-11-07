package auditjoin

import (
	"context"
	"testing"
	"time"
)

func TestJoinApprovalCountsOnlyThatApprovalsWrites(t *testing.T) {
	p := writeLog(t,
		`{"kind":"intent","method":"POST","path":"/a","approval_id":"A"}`,
		`{"kind":"done","method":"POST","path":"/a","approval_id":"A"}`,
		`{"kind":"intent","method":"POST","path":"/b","approval_id":"B"}`,
		`{"kind":"intent","method":"PUT","path":"/c"}`,
	)
	res := JoinApproval(context.Background(), p, stubApprovals{ids: map[string]struct{}{"A": {}}}, time.Time{}, "A")
	if res.Status != StatusOK || res.Writes != 1 || len(res.Unmatched) != 0 {
		t.Fatalf("A = %+v, want OK with 1 write (B and the id-less write are not A's)", res)
	}
	// B wrote but is not approved: UNMATCHED for B's step
	res = JoinApproval(context.Background(), p, stubApprovals{ids: map[string]struct{}{"A": {}}}, time.Time{}, "B")
	if res.Status != StatusUnmatched || res.Writes != 1 || len(res.Unmatched) != 1 {
		t.Fatalf("B = %+v, want UNMATCHED", res)
	}
	// the whole-log join is unchanged: 3 writes, B and the id-less one unmatched
	all := Join(context.Background(), p, stubApprovals{ids: map[string]struct{}{"A": {}}}, time.Time{})
	if all.Writes != 3 || len(all.Unmatched) != 2 {
		t.Fatalf("Join = %+v, want 3 writes / 2 unmatched", all)
	}
}

// MUTANT: return OK when the approval has no row and a step that bypassed
// the proxy would read as clean.
func TestJoinApprovalWithNoRowIsNotRun(t *testing.T) {
	p := writeLog(t, `{"kind":"intent","method":"POST","path":"/a","approval_id":"A"}`)
	res := JoinApproval(context.Background(), p, stubApprovals{ids: map[string]struct{}{"A": {}, "C": {}}}, time.Time{}, "C")
	if res.Status != StatusNotRun || res.Err == nil {
		t.Fatalf("C = %+v, want NOT_RUN naming the missing row", res)
	}
	if res := JoinApproval(context.Background(), p, stubApprovals{}, time.Time{}, ""); res.Status != StatusNotRun {
		t.Fatalf("empty approval id = %+v, want NOT_RUN", res)
	}
	if res := JoinApproval(context.Background(), writeLog(t, ""), stubApprovals{}, time.Time{}, "A"); res.Status != StatusNotRun {
		t.Fatalf("empty log = %+v, want NOT_RUN", res)
	}
}
