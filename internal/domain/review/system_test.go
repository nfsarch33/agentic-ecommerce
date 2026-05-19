package review_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/review"
)

func TestReview_Submit(t *testing.T) {
	t.Parallel()
	rs := review.NewReviewSystem()
	r := review.Review{
		ID:        "R1",
		ProductID: "P1",
		AuthorID:  "U1",
		Rating:    5,
		Body:      "Great product!",
	}
	if err := rs.Submit(r); err != nil {
		t.Fatalf("Submit: %v", err)
	}
}

func TestReview_InvalidRating(t *testing.T) {
	t.Parallel()
	rs := review.NewReviewSystem()
	err := rs.Submit(review.Review{ID: "R1", ProductID: "P1", AuthorID: "U1", Rating: 6, Body: "x"})
	if err == nil {
		t.Fatal("expected error for rating > 5")
	}
}

func TestReview_AverageRating(t *testing.T) {
	t.Parallel()
	rs := review.NewReviewSystem()
	rs.Submit(review.Review{ID: "R1", ProductID: "P1", AuthorID: "U1", Rating: 4, Body: "good"})
	rs.Submit(review.Review{ID: "R2", ProductID: "P1", AuthorID: "U2", Rating: 2, Body: "meh"})
	avg := rs.AverageRating("P1")
	if avg != 3.0 {
		t.Fatalf("expected average 3.0, got %.1f", avg)
	}
}

func TestReview_ModerationQueue(t *testing.T) {
	t.Parallel()
	rs := review.NewReviewSystem()
	rs.Submit(review.Review{ID: "R1", ProductID: "P1", AuthorID: "U1", Rating: 1, Body: "spam spam spam"})
	pending := rs.PendingModeration()
	if len(pending) == 0 {
		t.Fatal("expected review in moderation queue")
	}
}

func TestReview_Approve(t *testing.T) {
	t.Parallel()
	rs := review.NewReviewSystem()
	rs.Submit(review.Review{ID: "R1", ProductID: "P1", AuthorID: "U1", Rating: 1, Body: "spam"})
	if err := rs.Approve("R1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	got, _ := rs.Get("R1")
	if got.Status != review.StatusApproved {
		t.Fatalf("expected approved, got %s", got.Status)
	}
}

func TestReview_HelpfulVote(t *testing.T) {
	t.Parallel()
	rs := review.NewReviewSystem()
	rs.Submit(review.Review{ID: "R1", ProductID: "P1", AuthorID: "U1", Rating: 5, Body: "great"})
	rs.MarkHelpful("R1", "U2")
	rs.MarkHelpful("R1", "U3")
	got, _ := rs.Get("R1")
	if got.HelpfulVotes != 2 {
		t.Fatalf("expected 2 helpful votes, got %d", got.HelpfulVotes)
	}
}

func TestReview_DuplicateHelpfulVote(t *testing.T) {
	t.Parallel()
	rs := review.NewReviewSystem()
	rs.Submit(review.Review{ID: "R1", ProductID: "P1", AuthorID: "U1", Rating: 5, Body: "great"})
	rs.MarkHelpful("R1", "U2")
	rs.MarkHelpful("R1", "U2")
	got, _ := rs.Get("R1")
	if got.HelpfulVotes != 1 {
		t.Fatalf("expected 1 helpful vote (dedup), got %d", got.HelpfulVotes)
	}
}
