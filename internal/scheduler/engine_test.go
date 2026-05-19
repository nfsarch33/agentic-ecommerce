package scheduler_test

import (
	"errors"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/scheduler"
)

func TestScheduler_RegisterAndSchedule(t *testing.T) {
	t.Parallel()
	eng := scheduler.NewEngine()
	eng.Register("cleanup", func(_ interface{}) error { return nil })
	id, err := eng.Schedule("cleanup", "0 * * * *")
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if id == "" {
		t.Fatal("expected task ID")
	}
}

func TestScheduler_ExecuteRunsHandler(t *testing.T) {
	t.Parallel()
	ran := false
	eng := scheduler.NewEngine()
	eng.Register("job", func(_ interface{}) error { ran = true; return nil })
	id, _ := eng.Schedule("job", "* * * * *")
	result, err := eng.Execute(id)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success || !ran {
		t.Fatal("expected handler to run successfully")
	}
}

func TestScheduler_RetryOnFailure(t *testing.T) {
	t.Parallel()
	attempts := 0
	eng := scheduler.NewEngine()
	eng.Register("flaky", func(_ interface{}) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary")
		}
		return nil
	})
	id, _ := eng.Schedule("flaky", "* * * * *")
	if err := eng.Retry(id, 5); err != nil {
		t.Fatalf("Retry: %v", err)
	}
}

func TestScheduler_CancelStopsFutureRuns(t *testing.T) {
	t.Parallel()
	eng := scheduler.NewEngine()
	eng.Register("job", func(_ interface{}) error { return nil })
	id, _ := eng.Schedule("job", "* * * * *")
	if err := eng.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
}

func TestScheduler_InvalidCronError(t *testing.T) {
	t.Parallel()
	eng := scheduler.NewEngine()
	eng.Register("job", func(_ interface{}) error { return nil })
	if _, err := eng.Schedule("job", "not-a-cron"); err == nil {
		t.Fatal("expected invalid cron error")
	}
}

func TestScheduler_DuplicateRegistrationError(t *testing.T) {
	t.Parallel()
	eng := scheduler.NewEngine()
	eng.Register("job", func(_ interface{}) error { return nil })
	if err := eng.Register("job", func(_ interface{}) error { return nil }); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestScheduler_MaxRetriesExceeded(t *testing.T) {
	t.Parallel()
	eng := scheduler.NewEngine()
	eng.Register("fail", func(_ interface{}) error { return errors.New("always fails") })
	id, _ := eng.Schedule("fail", "* * * * *")
	if err := eng.Retry(id, 2); err == nil {
		t.Fatal("expected max retries exceeded error")
	}
}
