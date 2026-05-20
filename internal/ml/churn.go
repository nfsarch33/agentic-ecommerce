package ml

import "time"

type UserActivity struct {
	UserID        string
	LastActive    time.Time
	SessionCount  int
	PurchaseCount int
	EmailOpens    int
}

type ChurnRisk struct {
	UserID      string
	Score       float64 // 0.0-1.0
	Indicators  []string
	Recommended string
}

// EngagementScore returns a 0.0-1.0 score based on recency, sessions, and purchases.
func EngagementScore(a UserActivity, now time.Time) float64 {
	daysSince := now.Sub(a.LastActive).Hours() / 24
	recency := 1.0
	if daysSince > 90 {
		recency = 0.0
	} else if daysSince > 30 {
		recency = 0.3
	} else if daysSince > 7 {
		recency = 0.6
	}

	session := float64(a.SessionCount) / 20.0
	if session > 1.0 {
		session = 1.0
	}
	purchase := float64(a.PurchaseCount) / 5.0
	if purchase > 1.0 {
		purchase = 1.0
	}

	return (recency*0.5 + session*0.3 + purchase*0.2)
}

// RiskIndicators returns human-readable reasons a user is at churn risk.
func RiskIndicators(a UserActivity, now time.Time) []string {
	var out []string
	daysSince := now.Sub(a.LastActive).Hours() / 24
	if daysSince > 30 {
		out = append(out, "inactive_30d")
	}
	if a.SessionCount < 2 {
		out = append(out, "low_sessions")
	}
	if a.PurchaseCount == 0 {
		out = append(out, "no_purchases")
	}
	if a.EmailOpens == 0 {
		out = append(out, "no_email_opens")
	}
	return out
}

// RetentionAction maps a churn score to a recommended action.
func RetentionAction(score float64) string {
	if score >= 0.7 {
		return "no_action"
	}
	if score >= 0.4 {
		return "send_discount"
	}
	return "personal_outreach"
}

// AnalyseChurn scores a batch of users for churn risk.
func AnalyseChurn(users []UserActivity, now time.Time) []ChurnRisk {
	out := make([]ChurnRisk, len(users))
	for i, u := range users {
		score := EngagementScore(u, now)
		// Invert engagement to get churn risk
		churnScore := 1.0 - score
		out[i] = ChurnRisk{
			UserID:      u.UserID,
			Score:       churnScore,
			Indicators:  RiskIndicators(u, now),
			Recommended: RetentionAction(score),
		}
	}
	return out
}

// CohortRetention computes the fraction of users still active (within 30d) grouped by cohort month.
func CohortRetention(users []UserActivity, cohortFn func(UserActivity) string, now time.Time) map[string]float64 {
	counts := make(map[string]int)
	active := make(map[string]int)
	for _, u := range users {
		c := cohortFn(u)
		counts[c]++
		if now.Sub(u.LastActive).Hours()/24 <= 30 {
			active[c]++
		}
	}
	result := make(map[string]float64, len(counts))
	for k, total := range counts {
		if total > 0 {
			result[k] = float64(active[k]) / float64(total)
		}
	}
	return result
}
