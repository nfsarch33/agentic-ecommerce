package config

import (
	"errors"
	"hash/fnv"
	"sync"
)

var ErrFlagNotDefined = errors.New("feature flag not defined")

type flagDef struct {
	name       string
	defaultVal bool
	rolloutPct int
	targets    map[string]bool // userID -> override
}

type FlagStore struct {
	mu    sync.RWMutex
	flags map[string]*flagDef
}

func NewFlagStore() *FlagStore {
	return &FlagStore{flags: make(map[string]*flagDef)}
}

func (fs *FlagStore) Define(name string, defaultVal bool) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.flags[name] = &flagDef{name: name, defaultVal: defaultVal, rolloutPct: -1, targets: make(map[string]bool)}
	return nil
}

func (fs *FlagStore) SetRollout(name string, pct int) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if f, ok := fs.flags[name]; ok {
		f.rolloutPct = pct
	}
}

func (fs *FlagStore) TargetUser(name, userID string, enabled bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if f, ok := fs.flags[name]; ok {
		f.targets[userID] = enabled
	}
}

func (fs *FlagStore) Evaluate(name, userID string) (bool, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	f, ok := fs.flags[name]
	if !ok {
		return false, ErrFlagNotDefined
	}
	// User-specific override takes precedence
	if v, targeted := f.targets[userID]; targeted {
		return v, nil
	}
	// Rollout percentage
	if f.rolloutPct >= 0 {
		h := fnv.New32a()
		h.Write([]byte(name + ":" + userID))
		bucket := int(h.Sum32() % 100)
		return bucket < f.rolloutPct, nil
	}
	return f.defaultVal, nil
}
