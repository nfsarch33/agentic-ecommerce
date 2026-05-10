package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// runApp dispatches an argv slice to the matching subcommand handler.
// args[0] is the binary name; args[1] is the subcommand. Pure
// function of (ctx, args, deps) so tests can drive every branch with
// a fake deps and fake argv.
func runApp(ctx context.Context, args []string, deps appDeps) int {
	if len(args) < 2 {
		printUsage(deps.stderr)
		return 2
	}
	switch args[1] {
	case "doctor":
		return runDoctor(ctx, args[2:], deps)
	case "tenant":
		return runTenant(ctx, args[2:], deps)
	case "plugin":
		return runPlugin(ctx, args[2:], deps)
	case "skill":
		return runSkill(ctx, args[2:], deps)
	case "version", "--version", "-v":
		return runVersion(deps)
	case "help", "--help", "-h":
		printUsage(deps.stdout)
		return 0
	default:
		fmt.Fprintf(deps.stderr, "ec-cli: unknown subcommand %q\n\n", args[1])
		printUsage(deps.stderr)
		return 2
	}
}

func printUsage(w writer) {
	fmt.Fprint(w, `ec-cli -- Agentic Ecommerce developer experience CLI

Usage:
  ec-cli <subcommand> [args...]

Subcommands:
  doctor                          environment diagnostics
  tenant create --slug --name --plan
                                  provision a tenant via the registration API
  plugin validate --path <dir>    validate a plugin's manifest and run sandbox smoke
  skill quality-check <path>      validate a SKILL.md against quality criteria
  skill codex-gen <path> <outdir> generate a Codex-compatible skill variant
  version                         print binary metadata
  help                            show this message

Env:
  EC_API_URL                      mc-api base URL (default http://127.0.0.1:8080)
  EC_ADMIN_TOKEN                  admin bearer token (required by tenant create)
  ECOMMERCE_DB_URL                postgres dsn (used by doctor)
  ECOMMERCE_REDIS_URL             redis url (used by doctor)
  ECOMMERCE_TEMPORAL_ADDR         temporal address (used by doctor)
`)
}

// nowFromEnv returns the current UTC instant in RFC3339 nanos. Kept
// in a package-level var so tests can swap a deterministic clock
// without touching time.Now globally.
func nowFromEnv() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// writer is a tiny alias used to keep the appDeps signature flexible
// (deps.stdout and deps.stderr both satisfy this).
type writer interface {
	Write(p []byte) (int, error)
}

// trimEmpty returns s with surrounding whitespace stripped. It exists
// so subcommand handlers can write `if trimEmpty(input) == ""` and
// match the same emptiness rules across handlers.
func trimEmpty(s string) string { return strings.TrimSpace(s) }
