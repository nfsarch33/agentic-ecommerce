package auditjoin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type stubApprovals struct {
	ids  map[string]struct{}
	fail bool
}

func (s stubApprovals) ApprovedIDs(context.Context, time.Time) (map[string]struct{}, error) {
	if s.fail {
		return nil, os.ErrNotExist
	}
	return s.ids, nil
}

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "audit.ndjson")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	return p
}

// MUTANT: treat an empty log as a clean day (StatusOK with 0 unmatched) and
// this goes red — a dead proxy would masquerade as compliance.
func TestEmptyLogIsNotRunNeverZero(t *testing.T) {
	p := writeLog(t, "")
	res := Join(context.Background(), p, stubApprovals{ids: map[string]struct{}{}}, time.Time{})
	if res.Status != StatusNotRun {
		t.Fatalf("empty log = %v, want NOT_RUN (never 0)", res.Status)
	}
	if !os.IsNotExist(Join(context.Background(), filepath.Join(t.TempDir(), "absent.ndjson"), stubApprovals{ids: map[string]struct{}{}}, time.Time{}).Err) &&
		Join(context.Background(), filepath.Join(t.TempDir(), "absent.ndjson"), stubApprovals{ids: map[string]struct{}{}}, time.Time{}).Status != StatusNotRun {
		t.Fatalf("absent log must also be NOT_RUN")
	}
}

// MUTANT: match reads as well as writes (or drop the approved-set lookup) and
// the planted row count drifts; this pins exactly 1 unmatched for 1 planted.
func TestPlantedUnapprovedWriteIsExactlyOneUnmatched(t *testing.T) {
	p := writeLog(t,
		`{"ts":"2026-10-01T20:00:00Z","method":"GET","path":"/wp-json/wc/v3/products","status":200}`,
		`{"ts":"2026-10-01T20:01:00Z","method":"GET","path":"/wp-json/wc/v3/products?sku=X","status":200}`,
		`{"ts":"2026-10-01T20:02:00Z","method":"PUT","path":"/wp-json/wc/v3/products/7","approval_id":"ap-approved","status":200}`,
		`{"ts":"2026-10-01T20:03:00Z","method":"POST","path":"/wp-json/wc/v3/products","approval_id":"ap-PLANTED","status":201}`,
	)
	res := Join(context.Background(), p, stubApprovals{ids: map[string]struct{}{"ap-approved": {}}}, time.Time{})
	if res.Status != StatusUnmatched || len(res.Unmatched) != 1 {
		t.Fatalf("planted write: status=%v unmatched=%v, want UNMATCHED with exactly 1", res.Status, res.Unmatched)
	}
	if !strings.Contains(res.Unmatched[0], "ap-PLANTED") {
		t.Fatalf("unmatched row does not name the planted approval: %v", res.Unmatched)
	}
	if res.Reads != 2 || res.Writes != 2 {
		t.Fatalf("reads=%d writes=%d, want 2/2", res.Reads, res.Writes)
	}
}

// MUTANT: skip the empty-approval branch and a headerless write (refused at
// the proxy, but hypothetically present) hides behind the set lookup.
func TestWriteWithoutApprovalIdIsUnmatched(t *testing.T) {
	p := writeLog(t, `{"ts":"2026-10-01T21:00:00Z","method":"DELETE","path":"/wp-json/wc/v3/products/9","status":200}`)
	res := Join(context.Background(), p, stubApprovals{ids: map[string]struct{}{}}, time.Time{})
	if res.Status != StatusUnmatched || len(res.Unmatched) != 1 {
		t.Fatalf("no-approval write: %+v", res)
	}
}

// MUTANT: return OK on an approvals-reader failure and the nightly job would
// green-light a day it could not verify; this pins NOT_RUN on reader errors.
func TestApprovalsReaderFailureIsNotRun(t *testing.T) {
	p := writeLog(t, `{"ts":"2026-10-01T22:00:00Z","method":"PUT","path":"/wp-json/wc/v3/products/1","approval_id":"ap-1","status":200}`)
	res := Join(context.Background(), p, stubApprovals{fail: true}, time.Time{})
	if res.Status != StatusNotRun {
		t.Fatalf("reader failure = %v, want NOT_RUN", res.Status)
	}
}

func TestCleanDayIsZeroUnmatched(t *testing.T) {
	p := writeLog(t,
		`{"ts":"2026-10-01T23:00:00Z","method":"GET","path":"/wp-json/wc/v3/products","status":200}`,
		`{"ts":"2026-10-01T23:01:00Z","method":"POST","path":"/wp-json/wc/v3/products","approval_id":"ap-1","status":201}`,
	)
	res := Join(context.Background(), p, stubApprovals{ids: map[string]struct{}{"ap-1": {}}}, time.Time{})
	if res.Status != StatusOK || len(res.Unmatched) != 0 || res.Writes != 1 {
		t.Fatalf("clean day: %+v", res)
	}
}
