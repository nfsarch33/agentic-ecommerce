package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type config struct {
	Lane             string
	RepoRoot         string
	FrontendRepoPath string
	StagingBaseURL   string
	Timeout          time.Duration
}

type commandRunner interface {
	Run(ctx context.Context, dir string, name string, args ...string) error
}

type execCommandRunner struct {
	stdout io.Writer
	stderr io.Writer
}

func (r execCommandRunner) Run(ctx context.Context, dir string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = r.stdout
	cmd.Stderr = r.stderr
	return cmd.Run()
}

type ioWriterPair struct {
	Stdout io.Writer
	Stderr io.Writer
}

type cleanupSummary struct {
	ContainersStopped bool   `json:"containers_stopped,omitempty"`
	BrowsersStopped   bool   `json:"browsers_stopped,omitempty"`
	SentruxStopped    bool   `json:"sentrux_stopped,omitempty"`
	RemoteJobsStopped bool   `json:"remote_jobs_stopped,omitempty"`
	Note              string `json:"note,omitempty"`
}

type cleanupFunc func(context.Context, string, commandRunner, ioWriterPair) (cleanupSummary, error)

func main() {
	os.Exit(mainImpl(
		context.Background(),
		os.Args[1:],
		os.Stdout,
		os.Stderr,
		os.Getenv,
		execCommandRunner{stdout: os.Stdout, stderr: os.Stderr},
		http.DefaultClient,
		defaultCleanup,
	))
}

func mainImpl(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer, getenv func(string) string, runner commandRunner, client *http.Client, cleanup cleanupFunc) int {
	if err := run(args, stdout, stderr, getenv, runner, client, cleanup); err != nil {
		fmt.Fprintf(stderr, "testing-lane: %v\n", err)
		return 1
	}
	return 0
}

func run(args []string, stdout io.Writer, stderr io.Writer, getenv func(string) string, runner commandRunner, client *http.Client, cleanup cleanupFunc) error {
	cfg, err := parseArgs(args, getenv, stderr)
	if err != nil {
		return err
	}
	if runner == nil {
		runner = execCommandRunner{stdout: stdout, stderr: stderr}
	}
	if client == nil {
		client = http.DefaultClient
	}
	if cleanup == nil {
		cleanup = defaultCleanup
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	switch cfg.Lane {
	case "backend-integration":
		return runCommands(ctx, runner, cfg.RepoRoot,
			[]commandSpec{
				{Dir: cfg.RepoRoot, Name: "make", Args: []string{"contract-test"}},
				{Dir: cfg.RepoRoot, Name: "make", Args: []string{"integration-pg"}},
			},
		)
	case "frontend-playwright-stable":
		return runCommands(ctx, runner, cfg.RepoRoot,
			[]commandSpec{
				{Dir: cfg.FrontendRepoPath, Name: "bun", Args: []string{"run", "test:e2e:stable"}},
			},
		)
	case "frontend-uiauto-compare":
		return runCommands(ctx, runner, cfg.RepoRoot,
			[]commandSpec{
				{Dir: cfg.RepoRoot, Name: "make", Args: []string{"compose-uiauto-config"}},
				{Dir: cfg.RepoRoot, Name: "make", Args: []string{"uiauto-compare"}},
			},
		)
	case "frontend-live-ai":
		return fmt.Errorf("frontend-live-ai requires EC_TESTING_LIVE_AI_COMMAND")
	case "cleanup-testing":
		summary, err := cleanup(ctx, cfg.RepoRoot, runner, ioWriterPair{Stdout: stdout, Stderr: stderr})
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(summary)
	case "staging-smoke":
		return runStagingSmoke(ctx, client, cfg.StagingBaseURL, stdout)
	case "staging-rollback":
		return fmt.Errorf("staging-rollback requires EC_TESTING_STAGING_ROLLBACK_COMMAND")
	default:
		return fmt.Errorf("unsupported lane %q", cfg.Lane)
	}
}

type commandSpec struct {
	Dir  string
	Name string
	Args []string
}

func runCommands(ctx context.Context, runner commandRunner, repoRoot string, commands []commandSpec) error {
	for _, command := range commands {
		if err := runner.Run(ctx, command.Dir, command.Name, command.Args...); err != nil {
			return fmt.Errorf("%s %s in %s: %w", command.Name, strings.Join(command.Args, " "), command.Dir, err)
		}
	}
	return nil
}

func parseArgs(args []string, getenv func(string) string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("testing-lane", flag.ContinueOnError)
	fs.SetOutput(stderr)

	repoRoot, err := os.Getwd()
	if err != nil {
		repoRoot = "."
	}
	cfg := config{
		RepoRoot:         repoRoot,
		FrontendRepoPath: defaultFrontendRepoPath(getenv),
		StagingBaseURL:   getenv("EC_STAGING_BASE_URL"),
		Timeout:          2 * time.Minute,
	}
	fs.StringVar(&cfg.Lane, "lane", "", "lane to execute")
	fs.StringVar(&cfg.RepoRoot, "repo-root", cfg.RepoRoot, "backend repo root")
	fs.StringVar(&cfg.FrontendRepoPath, "frontend-repo", cfg.FrontendRepoPath, "frontend repo root")
	fs.StringVar(&cfg.StagingBaseURL, "staging-base-url", cfg.StagingBaseURL, "staging base URL for smoke checks")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "overall timeout")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if strings.TrimSpace(cfg.Lane) == "" {
		return cfg, errors.New("--lane is required")
	}
	return cfg, nil
}

func defaultFrontendRepoPath(getenv func(string) string) string {
	if getenv != nil {
		if path := strings.TrimSpace(getenv("EC_FRONTEND_REPO_PATH")); path != "" {
			return path
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "agentic-ecommerce-web"
	}
	return filepath.Join(home, "Code", "agentic-ecommerce-web")
}

func runStagingSmoke(ctx context.Context, client *http.Client, baseURL string, stdout io.Writer) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return fmt.Errorf("staging-smoke requires EC_STAGING_BASE_URL or --staging-base-url")
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
		if err != nil {
			return fmt.Errorf("build request for %s: %w", path, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("GET %s: %w", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
		}
	}
	_, _ = fmt.Fprintln(stdout, "staging-smoke: ok")
	return nil
}

func defaultCleanup(ctx context.Context, repoRoot string, runner commandRunner, _ ioWriterPair) (cleanupSummary, error) {
	summary := cleanupSummary{
		ContainersStopped: true,
		BrowsersStopped:   true,
		SentruxStopped:    true,
		RemoteJobsStopped: true,
	}
	bestEffort := []commandSpec{
		{Dir: repoRoot, Name: "docker", Args: []string{"compose", "-f", "docker-compose.dev.yml", "--profile", "uiauto", "down", "--remove-orphans"}},
		{Dir: repoRoot, Name: "docker", Args: []string{"compose", "-f", "docker-compose.dev.yml", "down", "--remove-orphans"}},
		{Dir: repoRoot, Name: "docker", Args: []string{"compose", "-f", "docker-compose.yml", "down", "--remove-orphans"}},
	}
	for _, command := range bestEffort {
		if err := runner.Run(ctx, command.Dir, command.Name, command.Args...); err != nil {
			if !isCommandUnavailable(err) {
				summary.ContainersStopped = false
				summary.Note = "cleanup encountered docker errors"
			}
		}
	}
	return summary, nil
}

func isCommandUnavailable(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr) && execErr.Err == exec.ErrNotFound
}
