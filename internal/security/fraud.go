package security

import (
	"errors"
	"sync"
)

var ErrOrderNotFound = errors.New("order not found")

type FraudOrder struct {
	ID      string
	Email   string
	IP      string
	Country string
	Amount  int
}

type RuleEngine struct {
	mu           sync.RWMutex
	blocklist    map[string]bool // email or IP
	flagged      map[string]bool // orderID
}

func NewRuleEngine() *RuleEngine {
	return &RuleEngine{
		blocklist: make(map[string]bool),
		flagged:   make(map[string]bool),
	}
}

func (re *RuleEngine) AddToBlocklist(identifier string) {
	re.mu.Lock()
	re.blocklist[identifier] = true
	re.mu.Unlock()
}

func (re *RuleEngine) RiskScore(order FraudOrder, history []FraudOrder) int {
	score := 0
	re.mu.RLock()
	isBlocklisted := re.blocklist[order.Email] || re.blocklist[order.IP]
	re.mu.RUnlock()

	if isBlocklisted {
		score += 80
	}
	// Velocity check: more than 3 orders from same email
	count := 0
	for _, h := range history {
		if h.Email == order.Email {
			count++
		}
	}
	if count > 3 {
		score += 30
	}
	// Amount threshold
	if order.Amount > 100000 {
		score += 20
	}
	// Geo mismatch (simplified: if IP country differs from order country)
	if order.Country == "XX" { // placeholder for unknown geo
		score += 15
	}
	if score > 100 {
		return 100
	}
	return score
}

func (re *RuleEngine) ManualReview(orderID string) error {
	re.mu.Lock()
	re.flagged[orderID] = true
	re.mu.Unlock()
	return nil
}

func (re *RuleEngine) IsFlagged(orderID string) bool {
	re.mu.RLock()
	defer re.mu.RUnlock()
	return re.flagged[orderID]
}
