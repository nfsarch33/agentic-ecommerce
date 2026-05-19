package testing_test

import (
	"errors"
	"strings"
	"testing"

	itesting "github.com/nfsarch33/helixon-ec/internal/testing"
)

func TestIntegration_SetupCreatesFixtures(t *testing.T) {
	t.Parallel()
	s := itesting.NewSuite()
	ran := false
	fixtures := []itesting.Fixture{{Name: "db", Setup: func() error { ran = true; return nil }}}
	if err := s.SetupFixtures(fixtures); err != nil {
		t.Fatalf("SetupFixtures: %v", err)
	}
	if !ran {
		t.Fatal("expected setup to run")
	}
}

func TestIntegration_RunSuiteCollectsResults(t *testing.T) {
	t.Parallel()
	s := itesting.NewSuite()
	tests := []itesting.TestCase{
		{Name: "pass", Run: func() error { return nil }},
		{Name: "fail", Run: func() error { return errors.New("bad") }},
	}
	result := s.RunSuite(tests)
	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
}

func TestIntegration_CleanupReverseOrder(t *testing.T) {
	t.Parallel()
	s := itesting.NewSuite()
	var order []string
	fixtures := []itesting.Fixture{
		{Name: "first", Teardown: func() error { order = append(order, "first"); return nil }},
		{Name: "second", Teardown: func() error { order = append(order, "second"); return nil }},
	}
	s.SetupFixtures(fixtures)
	s.Cleanup()
	if len(order) != 2 || order[0] != "second" {
		t.Fatalf("expected reverse teardown order, got %v", order)
	}
}

func TestIntegration_ReportFormat(t *testing.T) {
	t.Parallel()
	s := itesting.NewSuite()
	result := s.RunSuite([]itesting.TestCase{{Name: "ok", Run: func() error { return nil }}})
	report := itesting.Report(result)
	if !strings.Contains(report, "PASS") {
		t.Fatalf("expected PASS in report, got: %s", report)
	}
}

func TestIntegration_PartialFailureContinuesSuite(t *testing.T) {
	t.Parallel()
	s := itesting.NewSuite()
	tests := []itesting.TestCase{
		{Name: "fail", Run: func() error { return errors.New("oops") }},
		{Name: "pass", Run: func() error { return nil }},
	}
	result := s.RunSuite(tests)
	if len(result.Results) != 2 {
		t.Fatal("suite should run all tests even after failure")
	}
}

func TestIntegration_EmptySuite(t *testing.T) {
	t.Parallel()
	s := itesting.NewSuite()
	result := s.RunSuite(nil)
	if len(result.Results) != 0 {
		t.Fatalf("expected empty results, got %d", len(result.Results))
	}
}
