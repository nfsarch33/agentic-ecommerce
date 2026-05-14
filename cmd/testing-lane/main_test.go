package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls []commandCall
	fail  map[string]error
}

type commandCall struct {
	dir  string
	name string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, dir string, name string, args ...string) error {
	f.calls = append(f.calls, commandCall{dir: dir, name: name, args: append([]string(nil), args...)})
	if f.fail != nil {
		if err := f.fail[commandKey(dir, name, args...)]; err != nil {
			return err
		}
	}
	return nil
}

func commandKey(dir, name string, args ...string) string {
	return dir + "|" + name + "|" + strings.Join(args, " ")
}

func TestRunBackendIntegrationRunsExpectedCommands(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--lane=backend-integration", "--repo-root=/repo", "--frontend-repo=/frontend"},
		&stdout,
		&stderr,
		func(string) string { return "" },
		runner,
		nil,
		func(context.Context, string, commandRunner, ioWriterPair) (cleanupSummary, error) {
			t.Fatal("cleanup should not run")
			return cleanupSummary{}, nil
		},
	)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}
	want := []commandCall{
		{dir: "/repo", name: "make", args: []string{"contract-test"}},
		{dir: "/repo", name: "make", args: []string{"integration-pg"}},
	}
	assertCalls(t, runner.calls, want)
}

func TestRunFrontendPlaywrightStableUsesFrontendRepo(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--lane=frontend-playwright-stable", "--repo-root=/repo", "--frontend-repo=/frontend"},
		&stdout,
		&stderr,
		func(string) string { return "" },
		runner,
		nil,
		func(context.Context, string, commandRunner, ioWriterPair) (cleanupSummary, error) {
			t.Fatal("cleanup should not run")
			return cleanupSummary{}, nil
		},
	)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}
	want := []commandCall{
		{dir: "/frontend", name: "bun", args: []string{"run", "test:e2e:stable"}},
	}
	assertCalls(t, runner.calls, want)
}

func TestRunFrontendUIAutoCompareRunsConfigAndCompare(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--lane=frontend-uiauto-compare", "--repo-root=/repo", "--frontend-repo=/frontend"},
		&stdout,
		&stderr,
		func(string) string { return "" },
		runner,
		nil,
		func(context.Context, string, commandRunner, ioWriterPair) (cleanupSummary, error) {
			t.Fatal("cleanup should not run")
			return cleanupSummary{}, nil
		},
	)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}
	want := []commandCall{
		{dir: "/repo", name: "make", args: []string{"compose-uiauto-config"}},
		{dir: "/repo", name: "make", args: []string{"uiauto-compare"}},
	}
	assertCalls(t, runner.calls, want)
}

func TestRunFullStackE2ERunsRepoScript(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--lane=full-stack-e2e", "--repo-root=/repo", "--frontend-repo=/frontend"},
		&stdout,
		&stderr,
		func(string) string { return "" },
		runner,
		nil,
		func(context.Context, string, commandRunner, ioWriterPair) (cleanupSummary, error) {
			t.Fatal("cleanup should not run")
			return cleanupSummary{}, nil
		},
	)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}
	want := []commandCall{
		{dir: "/repo", name: "bash", args: []string{"scripts/ci/full_stack_e2e.sh"}},
	}
	assertCalls(t, runner.calls, want)
}

func TestRunCleanupTestingWritesJSONSummary(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--lane=cleanup-testing", "--repo-root=/repo"},
		&stdout,
		&stderr,
		func(string) string { return "" },
		runner,
		nil,
		func(_ context.Context, repoRoot string, gotRunner commandRunner, _ ioWriterPair) (cleanupSummary, error) {
			if repoRoot != "/repo" {
				t.Fatalf("repoRoot = %q want /repo", repoRoot)
			}
			if gotRunner != runner {
				t.Fatal("cleanup received unexpected runner")
			}
			return cleanupSummary{
				ContainersStopped: true,
				BrowsersStopped:   true,
				SentruxStopped:    true,
				RemoteJobsStopped: true,
				Note:              "clean",
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}
	text := stdout.String()
	for _, want := range []string{
		`"containers_stopped":true`,
		`"browsers_stopped":true`,
		`"sentrux_stopped":true`,
		`"remote_jobs_stopped":true`,
		`"note":"clean"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("cleanup summary missing %q in %q", want, text)
		}
	}
}

func TestRunStagingSmokeChecksHealthzAndReadyz(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz", "/readyz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--lane=staging-smoke", "--repo-root=/repo", "--staging-base-url=" + server.URL, "--timeout=2s"},
		&stdout,
		&stderr,
		func(string) string { return "" },
		&fakeRunner{},
		server.Client(),
		func(context.Context, string, commandRunner, ioWriterPair) (cleanupSummary, error) {
			t.Fatal("cleanup should not run")
			return cleanupSummary{}, nil
		},
	)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "staging-smoke: ok") {
		t.Fatalf("stdout missing smoke success: %q", stdout.String())
	}
}

func TestRunLiveAILaneErrorsWhenNotConfigured(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--lane=frontend-live-ai", "--repo-root=/repo"},
		&stdout,
		&stderr,
		func(string) string { return "" },
		&fakeRunner{},
		nil,
		func(context.Context, string, commandRunner, ioWriterPair) (cleanupSummary, error) {
			t.Fatal("cleanup should not run")
			return cleanupSummary{}, nil
		},
	)
	if err == nil {
		t.Fatal("expected missing configuration error")
	}
	if !strings.Contains(err.Error(), "EC_TESTING_LIVE_AI_COMMAND") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMainImplReturnsOneOnLaneFailure(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{
		fail: map[string]error{
			commandKey("/repo", "make", "contract-test"): errors.New("boom"),
		},
	}
	var stdout, stderr bytes.Buffer
	got := mainImpl(
		context.Background(),
		[]string{"--lane=backend-integration", "--repo-root=/repo"},
		&stdout,
		&stderr,
		func(string) string { return "" },
		runner,
		nil,
		func(context.Context, string, commandRunner, ioWriterPair) (cleanupSummary, error) {
			return cleanupSummary{}, nil
		},
	)
	if got != 1 {
		t.Fatalf("mainImpl exit=%d stderr=%s", got, stderr.String())
	}
	if !strings.Contains(stderr.String(), "testing-lane:") {
		t.Fatalf("stderr missing prefix: %q", stderr.String())
	}
}

func assertCalls(t *testing.T, got, want []commandCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("call count = %d want %d\n got=%#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].dir != want[i].dir || got[i].name != want[i].name || strings.Join(got[i].args, " ") != strings.Join(want[i].args, " ") {
			t.Fatalf("call[%d] = %#v want %#v", i, got[i], want[i])
		}
	}
}

func TestDefaultFrontendRepoPathUsesHomeCodeCanonicalPath(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	oldHomeEnv := os.Getenv("HOME")
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHomeEnv) })
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	got := defaultFrontendRepoPath(func(string) string { return "" })
	want := home + "/Code/agentic-ecommerce-web"
	if got != want {
		t.Fatalf("defaultFrontendRepoPath = %q want %q", got, want)
	}
}

func TestParseArgsTimeoutAndDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := parseArgs(
		[]string{"--lane=staging-smoke", "--timeout=5s"},
		func(key string) string {
			switch key {
			case "EC_FRONTEND_REPO_PATH":
				return "/frontend"
			case "EC_STAGING_BASE_URL":
				return "https://example.com"
			default:
				return ""
			}
		},
		&bytes.Buffer{},
	)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if cfg.Timeout != 5*time.Second {
		t.Fatalf("timeout = %s want 5s", cfg.Timeout)
	}
	if cfg.FrontendRepoPath != "/frontend" {
		t.Fatalf("frontendRepoPath = %q", cfg.FrontendRepoPath)
	}
	if cfg.StagingBaseURL != "https://example.com" {
		t.Fatalf("stagingBaseURL = %q", cfg.StagingBaseURL)
	}
}
