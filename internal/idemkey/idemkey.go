// Package idemkey derives the idempotency key for publishing one approved draft
// to a store, and decides whether the store's live copy already matches the draft.
package idemkey

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// Sentinel errors returned by Key, checked in the order listed.
var (
	ErrEmptyDraftID = errors.New("idemkey: draft id is empty or whitespace")
	ErrBadDraftID   = errors.New("idemkey: draft id contains ':'")
	ErrNoFields     = errors.New("idemkey: fields is nil or empty")
)

// Key returns draftID + ":" + the lowercase hex SHA-256 of the canonical
// encoding of fields, which is exactly what encoding/json.Marshal produces for
// a map[string]string (keys sorted, standard escaping). Values are hashed as
// given: no trimming, no case folding.
//
// Errors are checked in this order: empty/whitespace id, then ':' in the id,
// then no fields. On error the returned key is "".
func Key(draftID string, fields map[string]string) (string, error) {
	if strings.TrimSpace(draftID) == "" {
		return "", ErrEmptyDraftID
	}
	if strings.Contains(draftID, ":") {
		return "", ErrBadDraftID
	}
	if len(fields) == 0 {
		return "", ErrNoFields
	}
	b, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return draftID + ":" + hex.EncodeToString(sum[:]), nil
}

// Matches reports whether every key in desired is present in live with the two
// values equal after strings.TrimSpace on both. Keys that exist only in live
// are ignored. An empty or nil desired matches nothing and returns false.
func Matches(live, desired map[string]string) bool {
	if len(desired) == 0 {
		return false
	}
	for k, v := range desired {
		have, ok := live[k]
		if !ok {
			return false
		}
		if strings.TrimSpace(have) != strings.TrimSpace(v) {
			return false
		}
	}
	return true
}
