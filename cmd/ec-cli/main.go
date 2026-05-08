// ec-cli is the v2.9.0 developer experience command-line tool. It
// runs as a thin shell over the host's existing services so plugin
// authors and operators can inspect the cluster, provision tenants,
// and validate plugins without writing custom Go drivers.
//
// Subcommands:
//
//	ec-cli doctor              -- environment diagnostics (DB/Redis/Temporal/env)
//	ec-cli tenant create ...   -- provision a tenant via the registration API
//	ec-cli plugin validate ... -- validate a plugin's manifest + smoke test
//	ec-cli version             -- print binary metadata
//
// Pattern: mainImpl + appDeps so the binary stays test-friendly. The
// stdlib `flag` package handles subcommand routing (we deliberately
// avoid pulling in cobra to keep the dependency surface minimal --
// the existing 6 binaries follow the same convention).
package main

import (
	"context"
	"io"
	"os"
)

// version, commit, buildTime are stamped at build time via -ldflags.
// Defaults match the existing cmd/* binaries.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	os.Exit(mainImpl(context.Background(), os.Args, os.Stdout, os.Stderr, os.Getenv))
}

// mainImpl is the testable entry point. It returns the process exit
// code so main() reduces to os.Exit(mainImpl(...)). Tests inject
// args, getenv, and writers to drive every branch.
func mainImpl(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	deps := appDeps{
		stdout: stdout,
		stderr: stderr,
		getenv: getenv,
		now:    nowFromEnv,
	}
	return runApp(ctx, args, deps)
}

// appDeps bundles the io and env-resolution surface used by every
// subcommand handler. Splitting it out keeps each handler a pure
// function of (deps, args) and makes table-driven tests trivial.
type appDeps struct {
	stdout io.Writer
	stderr io.Writer
	getenv func(string) string
	now    func() string
}
