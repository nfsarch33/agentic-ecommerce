package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// tenantCreateRequest is the JSON body posted to the registration
// API. We post directly to the registration endpoint rather than
// invoking the workflow in-process so the CLI stays
// transport-agnostic and works against any cluster the operator can
// reach.
type tenantCreateRequest struct {
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Plan  string `json:"plan"`
	Email string `json:"email"`
}

type tenantCreateResponse struct {
	TenantID  string `json:"tenant_id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Plan      string `json:"plan"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func runTenant(ctx context.Context, args []string, deps appDeps) int {
	if len(args) < 1 {
		fmt.Fprintln(deps.stderr, "ec-cli tenant: subcommand required (create)")
		return 2
	}
	switch args[0] {
	case "create":
		return runTenantCreate(ctx, args[1:], deps)
	default:
		fmt.Fprintf(deps.stderr, "ec-cli tenant: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runTenantCreate(ctx context.Context, args []string, deps appDeps) int {
	fs := flag.NewFlagSet("tenant create", flag.ContinueOnError)
	fs.SetOutput(deps.stderr)
	slug := fs.String("slug", "", "kebab-case tenant slug (required)")
	name := fs.String("name", "", "human-readable tenant name (required)")
	plan := fs.String("plan", "starter", "billing plan slug")
	email := fs.String("email", "", "owner email (required)")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	timeoutSec := fs.Int("timeout-seconds", 10, "HTTP timeout in seconds")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	req := tenantCreateRequest{
		Slug:  trimEmpty(*slug),
		Name:  trimEmpty(*name),
		Plan:  trimEmpty(*plan),
		Email: trimEmpty(*email),
	}
	if err := validateTenantCreate(req); err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli tenant create: %v\n", err)
		return 2
	}
	token := trimEmpty(deps.getenv("EC_ADMIN_TOKEN"))
	if token == "" {
		fmt.Fprintln(deps.stderr, "ec-cli tenant create: EC_ADMIN_TOKEN env var required")
		return 2
	}

	resp, err := callTenantCreate(ctx, deps, req, token, time.Duration(*timeoutSec)*time.Second)
	if err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli tenant create: %v\n", err)
		return 1
	}

	if *jsonOut {
		if err := encodeJSON(deps.stdout, resp); err != nil {
			fmt.Fprintf(deps.stderr, "ec-cli tenant create: encode json: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(deps.stdout, "tenant created\n")
	fmt.Fprintf(deps.stdout, "  id     %s\n", resp.TenantID)
	fmt.Fprintf(deps.stdout, "  slug   %s\n", resp.Slug)
	fmt.Fprintf(deps.stdout, "  name   %s\n", resp.Name)
	fmt.Fprintf(deps.stdout, "  plan   %s\n", resp.Plan)
	fmt.Fprintf(deps.stdout, "  status %s\n", resp.Status)
	return 0
}

func validateTenantCreate(req tenantCreateRequest) error {
	if req.Slug == "" {
		return errors.New("--slug is required")
	}
	if req.Name == "" {
		return errors.New("--name is required")
	}
	if req.Email == "" {
		return errors.New("--email is required")
	}
	if !strings.Contains(req.Email, "@") {
		return fmt.Errorf("--email %q does not look like an email address", req.Email)
	}
	return nil
}

// httpDoer is the surface tests stub. Production wires &http.Client{}.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// httpDoerOverride is exported (lowercase, package-internal) so the
// test file can swap in a stub without touching environment globals.
var httpDoerOverride httpDoer

func callTenantCreate(ctx context.Context, deps appDeps, payload tenantCreateRequest, token string, timeout time.Duration) (tenantCreateResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return tenantCreateResponse{}, fmt.Errorf("marshal request: %w", err)
	}
	target := apiBaseURL(deps) + "/api/v1/tenants"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return tenantCreateResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	doer := httpDoerOverride
	if doer == nil {
		doer = &http.Client{Timeout: timeout}
	}
	resp, err := doer.Do(httpReq)
	if err != nil {
		return tenantCreateResponse{}, fmt.Errorf("post %s: %w", target, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return tenantCreateResponse{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tenantCreateResponse{}, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out tenantCreateResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return tenantCreateResponse{}, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}
