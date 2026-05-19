package review

import (
	"errors"
	"strings"
	"sync"
)

var (
	ErrInvalidRating = errors.New("rating must be between 1 and 5")
	ErrReviewNotFound = errors.New("review not found")
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

type Review struct {
	ID           string
	ProductID    string
	AuthorID     string
	Rating       int
	Body         string
	Status       Status
	HelpfulVotes int
}

type ReviewSystem struct {
	mu          sync.RWMutex
	reviews     map[string]*Review
	helpfulVotes map[string]map[string]bool // reviewID -> voterID set
}

func NewReviewSystem() *ReviewSystem {
	return &ReviewSystem{
		reviews:     make(map[string]*Review),
		helpfulVotes: make(map[string]map[string]bool),
	}
}

func (rs *ReviewSystem) Submit(r Review) error {
	if r.Rating < 1 || r.Rating > 5 {
		return ErrInvalidRating
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	r.Status = statusForReview(r)
	rs.reviews[r.ID] = &r
	rs.helpfulVotes[r.ID] = make(map[string]bool)
	return nil
}

// statusForReview auto-flags reviews containing repeated spam words for moderation.
func statusForReview(r Review) Status {
	lower := strings.ToLower(r.Body)
	spamWords := []string{"spam", "scam", "fake", "cheat"}
	for _, w := range spamWords {
		if strings.Count(lower, w) >= 2 {
			return StatusPending
		}
	}
	return StatusApproved
}

func (rs *ReviewSystem) Get(id string) (Review, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	r, ok := rs.reviews[id]
	if !ok {
		return Review{}, ErrReviewNotFound
	}
	return *r, nil
}

func (rs *ReviewSystem) Approve(id string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	r, ok := rs.reviews[id]
	if !ok {
		return ErrReviewNotFound
	}
	r.Status = StatusApproved
	return nil
}

func (rs *ReviewSystem) Reject(id string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	r, ok := rs.reviews[id]
	if !ok {
		return ErrReviewNotFound
	}
	r.Status = StatusRejected
	return nil
}

func (rs *ReviewSystem) PendingModeration() []Review {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	var result []Review
	for _, r := range rs.reviews {
		if r.Status == StatusPending {
			result = append(result, *r)
		}
	}
	return result
}

func (rs *ReviewSystem) AverageRating(productID string) float64 {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	sum, count := 0, 0
	for _, r := range rs.reviews {
		if r.ProductID == productID && r.Status == StatusApproved {
			sum += r.Rating
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return float64(sum) / float64(count)
}

func (rs *ReviewSystem) MarkHelpful(reviewID, voterID string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	r, ok := rs.reviews[reviewID]
	if !ok {
		return ErrReviewNotFound
	}
	voters := rs.helpfulVotes[reviewID]
	if voters[voterID] {
		return nil
	}
	voters[voterID] = true
	r.HelpfulVotes++
	return nil
}
