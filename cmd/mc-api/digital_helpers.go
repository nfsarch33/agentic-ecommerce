package main

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// customerOrFail resolves the authenticated customer's UUID for /me/*
// endpoints. The mapping is deterministic per (tenant, subject) so a
// customer's licences stay stable across sessions; admin /licenses
// endpoints pass an explicit customer_id instead.
//
// The implementation uses a UUIDv5 over the URL namespace with the
// tenant id and the actor subject as the input. UUIDv5 is collision-
// resistant for distinct (tenant, email) pairs.
//
// Customers without an authenticated actor (e.g. legacy api-token)
// are rejected with 401; the handler caller should ensure the route
// is gated by the customer-or-admin role bracket before reaching
// here.
func (s *server) customerOrFail(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	actor, ok := r.Context().Value(actorContextKey{}).(requestActor)
	if !ok || strings.TrimSpace(actor.Subject) == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return uuid.Nil, false
	}
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return uuid.Nil, false
	}
	return DeriveCustomerID(tenantID, actor.Subject), true
}

// DeriveCustomerID maps (tenantID, subject) to a stable UUID. Exposed
// so admin endpoints and tests can mint matching ids.
func DeriveCustomerID(tenantID, subject string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:agentic-ecommerce:digital:customer:"+strings.ToLower(strings.TrimSpace(tenantID))+":"+strings.ToLower(strings.TrimSpace(subject))))
}
