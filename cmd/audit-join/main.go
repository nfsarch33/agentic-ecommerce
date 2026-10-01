// Command audit-join is the nightly join of counting-proxy writes to
// approval rows (v18900-5 acceptance 3). Exit codes: 0 clean, 1 unmatched
// writes, 3 NOT_RUN (empty/missing log or approvals unreadable — never a
// clean 0). A Prometheus textfile gauge rides --textfile when set.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nfsarch33/agentic-ecommerce/internal/auditjoin"
)

func main() {
	var logPath, dsn, textfile, sinceArg string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--log":
			logPath = args[i+1]
			i++
		case "--dsn":
			dsn = args[i+1]
			i++
		case "--textfile":
			textfile = args[i+1]
			i++
		case "--since":
			sinceArg = args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "unknown flag %s\n", args[i])
			os.Exit(64)
		}
	}
	if logPath == "" || dsn == "" {
		fmt.Fprintln(os.Stderr, "usage: audit-join --log <audit.ndjson> --dsn <pg dsn> [--since 24h] [--textfile <path>]")
		os.Exit(64)
	}
	since := time.Now().Add(-24 * time.Hour)
	if sinceArg != "" {
		d, err := time.ParseDuration(sinceArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--since: %v\n", err)
			os.Exit(64)
		}
		since = time.Now().Add(-d)
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		fmt.Printf("status=NOT_RUN reads=0 writes=0 unmatched=0 error=%v\n", err)
		os.Exit(3)
	}
	defer pool.Close()

	res := auditjoin.Join(context.Background(), logPath, pgApprovals{pool}, since)
	writeTextfile(textfile, res)
	fmt.Printf("status=%s reads=%d writes=%d unmatched=%d\n", res.Status, res.Reads, res.Writes, len(res.Unmatched))
	for _, u := range res.Unmatched {
		fmt.Printf("unmatched %s\n", u)
	}
	if res.Err != nil {
		fmt.Printf("note %v\n", res.Err)
	}
	switch res.Status {
	case auditjoin.StatusOK:
		os.Exit(0)
	case auditjoin.StatusUnmatched:
		os.Exit(1)
	default:
		os.Exit(3)
	}
}

func writeTextfile(path string, res auditjoin.Result) {
	if path == "" {
		return
	}
	unmatched := len(res.Unmatched)
	status := string(res.Status)
	_ = os.WriteFile(path, []byte(fmt.Sprintf(
		"# HELP hlxn_audit_unmatched writes at the counting proxy with no approval row\n# TYPE hlxn_audit_unmatched gauge\nhlxn_audit_unmatched %d\n# HELP hlxn_audit_join_status the nightly join status (0 OK, 1 unmatched, 3 not_run)\n# TYPE hlxn_audit_join_status gauge\nhlxn_audit_join_status{status=%q} 1\n", unmatched, status)), 0o640)
}
