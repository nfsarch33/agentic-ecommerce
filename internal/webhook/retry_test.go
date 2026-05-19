package webhook_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/webhook"
)

func TestRetryScheduler_FirstAttempt(t *testing.T) {
	t.Parallel()
	sched := webhook.NewRetryScheduler(webhook.RetryConfig{MaxRetries: 6, BaseDelay: time.Second})
	delivery := webhook.WebhookDelivery{ID: "d1", URL: "http://example.com"}

	attempt, err := sched.Schedule(delivery)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if attempt.AttemptNumber != 1 {
		t.Fatalf("expected attempt 1, got %d", attempt.AttemptNumber)
	}
}

func TestRetryScheduler_BackoffTiming(t *testing.T) {
	t.Parallel()
	base := 100 * time.Millisecond
	sched := webhook.NewRetryScheduler(webhook.RetryConfig{MaxRetries: 6, BaseDelay: base})
	delivery := webhook.WebhookDelivery{ID: "d1", URL: "http://example.com", AttemptCount: 1}

	attempt, _ := sched.Schedule(delivery)
	// 2nd attempt: base * 2^1 = 200ms (+ jitter, so check >= 200ms and <= ~500ms)
	if attempt.NextRetryAt.Before(time.Now().Add(base * 2)) {
		t.Fatalf("backoff too short: NextRetryAt=%v", attempt.NextRetryAt)
	}
}

func TestRetryScheduler_MaxRetriesExhausted_DeadLetter(t *testing.T) {
	t.Parallel()
	sched := webhook.NewRetryScheduler(webhook.RetryConfig{MaxRetries: 3, BaseDelay: time.Second})
	delivery := webhook.WebhookDelivery{ID: "d1", URL: "http://example.com", AttemptCount: 3}

	_, err := sched.Schedule(delivery)
	if err == nil {
		t.Fatal("expected dead-letter error after max retries")
	}
}

func TestRetryScheduler_SuccessfulDelivery_NoRetry(t *testing.T) {
	t.Parallel()
	sched := webhook.NewRetryScheduler(webhook.RetryConfig{MaxRetries: 6, BaseDelay: time.Second})
	delivery := webhook.WebhookDelivery{
		ID:          "d1",
		URL:         "http://example.com",
		AttemptCount: 2,
		LastStatusCode: 200,
	}

	_, err := sched.Schedule(delivery)
	if err == nil {
		t.Fatal("successful delivery should return ErrAlreadyDelivered, not nil")
	}
}

func TestRetryScheduler_JitterBounds(t *testing.T) {
	t.Parallel()
	base := 100 * time.Millisecond
	sched := webhook.NewRetryScheduler(webhook.RetryConfig{MaxRetries: 6, BaseDelay: base})

	// schedule the same delivery 20 times; NextRetryAt times should vary (jitter)
	unique := make(map[int64]bool)
	for i := 0; i < 20; i++ {
		delivery := webhook.WebhookDelivery{ID: "d1", URL: "http://example.com", AttemptCount: 1}
		attempt, _ := sched.Schedule(delivery)
		unique[attempt.NextRetryAt.UnixNano()] = true
	}
	if len(unique) < 3 {
		t.Fatalf("jitter not working: only %d unique retry times out of 20", len(unique))
	}
}

func TestRetryScheduler_ExponentialGrowth(t *testing.T) {
	t.Parallel()
	base := 10 * time.Millisecond
	sched := webhook.NewRetryScheduler(webhook.RetryConfig{MaxRetries: 6, BaseDelay: base})

	var delays []time.Duration
	now := time.Now()
	for i := 0; i < 5; i++ {
		delivery := webhook.WebhookDelivery{ID: "d1", URL: "http://example.com", AttemptCount: i}
		attempt, _ := sched.Schedule(delivery)
		delays = append(delays, attempt.NextRetryAt.Sub(now))
	}
	// each delay should be larger than the previous
	for i := 1; i < len(delays); i++ {
		if delays[i] <= delays[i-1] {
			t.Fatalf("delay[%d]=%v not > delay[%d]=%v", i, delays[i], i-1, delays[i-1])
		}
	}
}

func TestRetryScheduler_DeadLetterAfterMax(t *testing.T) {
	t.Parallel()
	maxRetries := 3
	sched := webhook.NewRetryScheduler(webhook.RetryConfig{MaxRetries: maxRetries, BaseDelay: time.Millisecond})
	dl := sched.DeadLetterQueue()

	for i := 0; i < maxRetries; i++ {
		delivery := webhook.WebhookDelivery{ID: "d1", URL: "http://example.com", AttemptCount: i}
		sched.Schedule(delivery)
	}
	// The 4th attempt (AttemptCount == maxRetries) moves to dead-letter
	delivery := webhook.WebhookDelivery{ID: "d1", URL: "http://example.com", AttemptCount: maxRetries}
	sched.Schedule(delivery)

	if len(dl.Items()) == 0 {
		t.Fatal("expected dead-letter queue to have items")
	}
}
