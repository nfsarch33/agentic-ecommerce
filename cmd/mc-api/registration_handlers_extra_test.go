package main

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/registration"
	"github.com/nfsarch33/agentic-ecommerce/internal/tenant"
)

func TestWriteRegistrationErrorTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"email required", registration.ErrEmailRequired, 400},
		{"slug required", registration.ErrSlugRequired, 400},
		{"slug taken", registration.ErrSlugTaken, 409},
		{"token invalid", registration.ErrTokenInvalid, 401},
		{"token expired", registration.ErrTokenExpired, 401},
		{"already verified", registration.ErrAlreadyVerified, 409},
		{"already active", registration.ErrAlreadyActive, 409},
		{"invalid transition", registration.ErrInvalidTransition, 422},
		{"not found", registration.ErrRequestNotFound, 404},
		{"slug exists tenant", tenant.ErrTenantSlugAlreadyExists, 409},
		{"unknown", errors.New("boom"), 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeRegistrationError(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("err %v -> code %d, want %d", tc.err, rec.Code, tc.want)
			}
		})
	}
}
