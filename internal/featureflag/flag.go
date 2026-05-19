package featureflag

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
)

// Rule type constants.
const (
	RuleTypeUserID    = "user_id"
	RuleTypePercentage = "percentage"
	RuleTypeAttribute = "attribute"
)

// Rule defines a targeting rule for a feature flag.
type Rule struct {
	Type  string
	Value string
}

// Flag represents a feature flag with rollout and targeting configuration.
type Flag struct {
	Key         string
	Description string
	Enabled     bool
	Rollout     float64
	Rules       []Rule
	KillSwitch  bool
}

// Store is a thread-safe storage for feature flags.
type Store struct {
	mu    sync.RWMutex
	flags map[string]Flag
}

// NewStore returns an initialised Store.
func NewStore() *Store {
	return &Store{flags: make(map[string]Flag)}
}

// Set inserts or replaces a flag.
func (s *Store) Set(flag Flag) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flags[flag.Key] = flag
}

// Get retrieves a flag by key.
func (s *Store) Get(key string) (*Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.flags[key]
	if !ok {
		return nil, errors.New("featureflag: flag not found: " + key)
	}
	cp := f
	return &cp, nil
}

// Delete removes a flag.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.flags, key)
}

// List returns all flags.
func (s *Store) List() []Flag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Flag, 0, len(s.flags))
	for _, f := range s.flags {
		out = append(out, f)
	}
	return out
}

// Evaluator evaluates feature flags for a given user and context.
type Evaluator struct{}

// IsEnabled returns whether a flag is enabled for the given user.
func (e Evaluator) IsEnabled(store *Store, key, userID string, attributes map[string]string) bool {
	flag, err := store.Get(key)
	if err != nil {
		return false
	}

	// Kill switch always overrides.
	if flag.KillSwitch {
		return false
	}

	// Globally disabled.
	if !flag.Enabled {
		return false
	}

	// Check targeting rules first.
	for _, rule := range flag.Rules {
		switch rule.Type {
		case RuleTypeUserID:
			if rule.Value == userID {
				return true
			}
		case RuleTypeAttribute:
			// Value format: "key=value"
			k, v := parseAttributeRule(rule.Value)
			if attributes != nil {
				if av, ok := attributes[k]; ok && av == v {
					return true
				}
			}
		}
	}

	// Rollout: use deterministic hash of key+userID modulo 100.
	if flag.Rollout > 0 {
		h := userRolloutBucket(key, userID)
		if float64(h) < flag.Rollout*100 {
			return true
		}
	}

	return false
}

// userRolloutBucket returns a value in [0, 100) derived from key+userID.
func userRolloutBucket(flagKey, userID string) int {
	sum := sha256.Sum256([]byte(flagKey + userID))
	// Use first 4 bytes as uint32 then mod 100.
	v := (uint32(sum[0])<<24 | uint32(sum[1])<<16 | uint32(sum[2])<<8 | uint32(sum[3]))
	return int(v % 100)
}

// parseAttributeRule splits "key=value" into (key, value).
func parseAttributeRule(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// Ensure Evaluator is usable without a pointer receiver.
var _ = fmt.Sprintf
