package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/storeonboard"
)

// onboard.go — v18870-4: `ec-cli onboard --check --config store.json`
// verifies a customer-store onboarding record against the live store:
// minimum-scope REST keys, the agent Application Password's non-admin user,
// the signed data statement and the named approver. Secrets ride env vars
// (EC_STORE_KEY, EC_STORE_SECRET, EC_STORE_APP_PASSWORD) — never the config
// file, never argv.

type onboardConfigFile struct {
	StoreURL       string `json:"store_url"`
	KeyPermissions string `json:"key_permissions"`
	AgentUserLogin string `json:"agent_user_login"`
	AgentUserRole  string `json:"agent_user_role"`
	DataStatement  struct {
		Version  string `json:"version"`
		SignedAt string `json:"signed_at"`
	} `json:"data_statement"`
	Approver struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"approver"`
}

func runOnboard(ctx context.Context, args []string, deps appDeps) int {
	if len(args) < 1 || args[0] != "check" {
		fmt.Fprintln(deps.stderr, "ec-cli onboard: subcommand required (check)")
		return 2
	}
	fs := flag.NewFlagSet("onboard check", flag.ContinueOnError)
	fs.SetOutput(deps.stderr)
	cfgPath := fs.String("config", "", "path to the onboarding record JSON (required)")
	timeoutSec := fs.Int("timeout-seconds", 10, "per-request HTTP timeout")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *cfgPath == "" {
		fmt.Fprintln(deps.stderr, "ec-cli onboard check: --config is required")
		return 2
	}
	raw, err := os.ReadFile(*cfgPath)
	if err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli onboard check: cannot read config: %v\n", err)
		return 2
	}
	var file onboardConfigFile
	if err := json.Unmarshal(raw, &file); err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli onboard check: config must be JSON: %v\n", err)
		return 2
	}
	cfg := storeonboard.Config{
		StoreURL:       file.StoreURL,
		KeyPermissions: file.KeyPermissions,
		AgentUserLogin: file.AgentUserLogin,
		AgentUserRole:  file.AgentUserRole,
	}
	cfg.DataStatement.Version = file.DataStatement.Version
	cfg.DataStatement.SignedAt = file.DataStatement.SignedAt
	cfg.Approver.Name = file.Approver.Name
	cfg.Approver.Email = file.Approver.Email

	getenv := deps.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	sec := storeonboard.Secrets{
		ConsumerKey:    getenv("EC_STORE_KEY"),
		ConsumerSecret: getenv("EC_STORE_SECRET"),
		AppPassword:    getenv("EC_STORE_APP_PASSWORD"),
	}

	results := storeonboard.Check(ctx, cfg, sec, &http.Client{}, time.Duration(*timeoutSec)*time.Second)
	passed := 0
	for _, r := range results {
		status := "FAIL"
		if r.Pass {
			status = "PASS"
			passed++
		}
		fmt.Fprintf(deps.stdout, "%s %-15s %s\n", status, r.Name, r.Note)
	}
	fmt.Fprintf(deps.stdout, "ONBOARDING CHECK: %d/%d PASS\n", passed, len(results))
	if !storeonboard.AllPassed(results) {
		return 1
	}
	return 0
}
