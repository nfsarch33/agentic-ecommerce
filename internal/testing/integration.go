package testing

import (
	"fmt"
	"strings"
	"sync"
)

type Fixture struct {
	Name  string
	Setup func() error
	Teardown func() error
}

type TestCase struct {
	Name string
	Run  func() error
}

type CaseResult struct {
	Name   string
	Passed bool
	Error  error
}

type SuiteResult struct {
	Results []CaseResult
}

type ProgressReport struct {
	Total     int
	Completed int
	Failed    int
}

type Suite struct {
	mu       sync.Mutex
	fixtures []Fixture
}

func NewSuite() *Suite { return &Suite{} }

func (s *Suite) SetupFixtures(fixtures []Fixture) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fixtures = fixtures
	for _, f := range fixtures {
		if f.Setup != nil {
			if err := f.Setup(); err != nil {
				return fmt.Errorf("setup %s: %w", f.Name, err)
			}
		}
	}
	return nil
}

func (s *Suite) RunSuite(tests []TestCase) SuiteResult {
	var results []CaseResult
	for _, tc := range tests {
		err := tc.Run()
		results = append(results, CaseResult{Name: tc.Name, Passed: err == nil, Error: err})
	}
	return SuiteResult{Results: results}
}

func (s *Suite) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Teardown in reverse order
	for i := len(s.fixtures) - 1; i >= 0; i-- {
		if s.fixtures[i].Teardown != nil {
			if err := s.fixtures[i].Teardown(); err != nil {
				return err
			}
		}
	}
	return nil
}

func Report(result SuiteResult) string {
	var sb strings.Builder
	passed, failed := 0, 0
	for _, r := range result.Results {
		if r.Passed {
			passed++
			fmt.Fprintf(&sb, "PASS: %s\n", r.Name)
		} else {
			failed++
			fmt.Fprintf(&sb, "FAIL: %s (%v)\n", r.Name, r.Error)
		}
	}
	fmt.Fprintf(&sb, "Total: %d Passed: %d Failed: %d", len(result.Results), passed, failed)
	return sb.String()
}

func Progress(result SuiteResult) ProgressReport {
	total := len(result.Results)
	failed := 0
	for _, r := range result.Results {
		if !r.Passed {
			failed++
		}
	}
	return ProgressReport{Total: total, Completed: total, Failed: failed}
}
