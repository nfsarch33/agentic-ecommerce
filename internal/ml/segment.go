package ml

import (
	"errors"
	"sync"
	"time"
)

var ErrUnknownSegment = errors.New("unknown segment")

type Order struct {
	UserID    string
	Revenue   int
	CreatedAt time.Time
}

type RFMResult struct {
	UserID    string
	Recency   int // days since last order
	Frequency int // total orders
	Monetary  int // total revenue
}

type SegmentRule struct {
	Name      string
	MinFreq   int
	MinMoney  int
	MaxRecency int // days
}

type CohortStat struct {
	Count    int
	AvgMonetary int
}

type SegmentStore struct {
	mu          sync.RWMutex
	transitions map[string][]string
}

func NewSegmentStore() *SegmentStore {
	return &SegmentStore{transitions: make(map[string][]string)}
}

func RFMScore(orders []Order) map[string]RFMResult {
	results := make(map[string]RFMResult)
	now := time.Now()
	for _, o := range orders {
		r := results[o.UserID]
		r.UserID = o.UserID
		days := int(now.Sub(o.CreatedAt).Hours() / 24)
		if r.Recency == 0 || days < r.Recency {
			r.Recency = days
		}
		r.Frequency++
		r.Monetary += o.Revenue
		results[o.UserID] = r
	}
	return results
}

var SegmentRules = []SegmentRule{
	{Name: "VIP", MinFreq: 5, MinMoney: 50000, MaxRecency: 30},
	{Name: "Regular", MinFreq: 2, MinMoney: 10000, MaxRecency: 90},
	{Name: "AtRisk", MinFreq: 1, MinMoney: 0, MaxRecency: 180},
	{Name: "Lost", MinFreq: 1, MinMoney: 0, MaxRecency: 99999},
}

func ClusterAssign(rfm RFMResult, rules []SegmentRule) string {
	for _, rule := range rules {
		if rfm.Frequency >= rule.MinFreq &&
			rfm.Monetary >= rule.MinMoney &&
			rfm.Recency <= rule.MaxRecency {
			return rule.Name
		}
	}
	return "Unknown"
}

func (ss *SegmentStore) Migrate(userID, from, to string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.transitions[userID] = append(ss.transitions[userID], from+"->"+to)
	return nil
}

func CohortAnalysis(users []RFMResult, period string) map[string]CohortStat {
	groups := make(map[string]CohortStat)
	for _, u := range users {
		key := period + ":" + ClusterAssign(u, SegmentRules)
		stat := groups[key]
		stat.Count++
		stat.AvgMonetary = (stat.AvgMonetary*(stat.Count-1) + u.Monetary) / stat.Count
		groups[key] = stat
	}
	return groups
}
