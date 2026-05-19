package notification

import "sync"

type preferenceKey struct {
	userID    string
	channel   string
	eventType string
}

// PreferenceStore persists per-user, per-channel, per-event notification preferences.
type PreferenceStore struct {
	mu              sync.RWMutex
	eventPrefs      map[preferenceKey]bool
	channelDefaults map[string]bool // "userID:channel" -> enabled
}

func NewPreferenceStore() *PreferenceStore {
	return &PreferenceStore{
		eventPrefs:      make(map[preferenceKey]bool),
		channelDefaults: make(map[string]bool),
	}
}

// SetPreference sets a per-event preference for a user on a channel.
func (s *PreferenceStore) SetPreference(userID, channel, eventType string, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventPrefs[preferenceKey{userID, channel, eventType}] = enabled
}

// SetChannelDefault sets the default for all events on a channel for a user.
func (s *PreferenceStore) SetChannelDefault(userID, channel string, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channelDefaults[userID+":"+channel] = enabled
}

// ShouldNotify returns true if the user should receive a notification.
// Event-specific preferences override channel defaults; channel defaults override the
// global default (true).
func (s *PreferenceStore) ShouldNotify(userID, channel, eventType string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := preferenceKey{userID, channel, eventType}
	if pref, ok := s.eventPrefs[key]; ok {
		return pref
	}
	if def, ok := s.channelDefaults[userID+":"+channel]; ok {
		return def
	}
	return true
}
