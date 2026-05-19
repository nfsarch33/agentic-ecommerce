package compliance

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrGDPRUserNotFound  = errors.New("gdpr: user not found")
	ErrRetentionHold     = errors.New("gdpr: deletion blocked by retention hold")
)

// DataInventory catalogs all data locations for a user.
type DataInventory struct {
	UserID    string
	Locations []DataLocationEntry
}

// DataLocationEntry identifies a specific data location.
type DataLocationEntry struct {
	System string
	Table  string
	Fields []string
}

// DeletionReceipt proves user data was deleted.
type DeletionReceipt struct {
	UserID          string
	DeletedAt       time.Time
	SystemsAffected int
}

// ConsentEntry tracks an individual consent decision.
type ConsentEntry struct {
	UserID     string
	Purpose    string
	Granted    bool
	RecordedAt time.Time
}

// GDPRManager provides right-to-erasure and portability operations.
type GDPRManager struct {
	mu       sync.Mutex
	userData map[string]map[string][]byte
	consents map[string][]ConsentEntry
	holds    map[string]bool
}

func NewGDPRManager() *GDPRManager {
	return &GDPRManager{
		userData: make(map[string]map[string][]byte),
		consents: make(map[string][]ConsentEntry),
		holds:    make(map[string]bool),
	}
}

func (g *GDPRManager) AddUserData(userID, system string, data []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.userData[userID] == nil {
		g.userData[userID] = make(map[string][]byte)
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	g.userData[userID][system] = cp
}

func (g *GDPRManager) SetRetentionHold(userID string, hold bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.holds[userID] = hold
}

func (g *GDPRManager) DataMap(_ interface{}, userID string) (DataInventory, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	systems, ok := g.userData[userID]
	if !ok {
		return DataInventory{}, ErrGDPRUserNotFound
	}
	inv := DataInventory{UserID: userID}
	for system := range systems {
		inv.Locations = append(inv.Locations, DataLocationEntry{
			System: system,
			Table:  system + "_table",
			Fields: []string{"id", "data"},
		})
	}
	return inv, nil
}

// ExportUserData packages all user data for portability (distinct from existing ExportBundle).
func (g *GDPRManager) ExportUserData(_ interface{}, userID string) (map[string][]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	systems, ok := g.userData[userID]
	if !ok {
		return nil, ErrGDPRUserNotFound
	}
	out := make(map[string][]byte, len(systems))
	for sys, data := range systems {
		cp := make([]byte, len(data))
		copy(cp, data)
		out[sys] = cp
	}
	return out, nil
}

func (g *GDPRManager) DeleteUserData(_ interface{}, userID string) (DeletionReceipt, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.holds[userID] {
		return DeletionReceipt{}, ErrRetentionHold
	}
	systems, ok := g.userData[userID]
	if !ok {
		return DeletionReceipt{}, ErrGDPRUserNotFound
	}
	count := len(systems)
	delete(g.userData, userID)
	delete(g.consents, userID)
	return DeletionReceipt{UserID: userID, DeletedAt: time.Now(), SystemsAffected: count}, nil
}

func (g *GDPRManager) RecordConsent(_ interface{}, userID, purpose string, granted bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.consents[userID] = append(g.consents[userID], ConsentEntry{
		UserID:     userID,
		Purpose:    purpose,
		Granted:    granted,
		RecordedAt: time.Now(),
	})
	return nil
}

func (g *GDPRManager) GetConsents(userID string) []ConsentEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	cp := make([]ConsentEntry, len(g.consents[userID]))
	copy(cp, g.consents[userID])
	return cp
}

func (g *GDPRManager) HasData(userID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.userData[userID]
	return ok
}
