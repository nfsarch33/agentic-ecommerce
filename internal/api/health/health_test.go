package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/api/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCheck struct {
	name string
	err  error
}

func (s *stubCheck) Name() string                { return s.name }
func (s *stubCheck) Check(context.Context) error { return s.err }

func TestLiveness_AlwaysOK(t *testing.T) {
	h := health.NewHandler(nil)
	rec := httptest.NewRecorder()
	h.Liveness(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp health.Response
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, health.StatusOK, resp.Status)
}

func TestReadiness_AllChecksPass(t *testing.T) {
	checks := []health.HealthCheck{
		&health.PostgresCheck{PingFunc: func(context.Context) error { return nil }},
		&health.RedisCheck{PingFunc: func(context.Context) error { return nil }},
		&health.TemporalCheck{PingFunc: func(context.Context) error { return nil }},
	}
	h := health.NewHandler(checks)
	rec := httptest.NewRecorder()
	h.Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp health.Response
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, health.StatusReady, resp.Status)
	assert.Equal(t, health.CheckStatusOK, resp.Checks["postgres"].Status)
	assert.Equal(t, health.CheckStatusOK, resp.Checks["redis"].Status)
	assert.Equal(t, health.CheckStatusOK, resp.Checks["temporal"].Status)
}

func TestReadiness_PostgresDown(t *testing.T) {
	checks := []health.HealthCheck{
		&health.PostgresCheck{PingFunc: func(context.Context) error { return errors.New("connection refused") }},
		&health.RedisCheck{PingFunc: func(context.Context) error { return nil }},
		&health.TemporalCheck{PingFunc: func(context.Context) error { return nil }},
	}
	h := health.NewHandler(checks)
	rec := httptest.NewRecorder()
	h.Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var resp health.Response
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, health.StatusNotReady, resp.Status)
	assert.Equal(t, health.CheckStatusFail, resp.Checks["postgres"].Status)
	assert.Contains(t, resp.Checks["postgres"].Error, "connection refused")
}

func TestReadiness_RedisDown(t *testing.T) {
	checks := []health.HealthCheck{
		&health.PostgresCheck{PingFunc: func(context.Context) error { return nil }},
		&health.RedisCheck{PingFunc: func(context.Context) error { return errors.New("redis: timeout") }},
		&health.TemporalCheck{PingFunc: func(context.Context) error { return nil }},
	}
	h := health.NewHandler(checks)
	rec := httptest.NewRecorder()
	h.Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var resp health.Response
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, health.StatusNotReady, resp.Status)
	assert.Equal(t, health.CheckStatusFail, resp.Checks["redis"].Status)
}

func TestReadiness_TemporalDown(t *testing.T) {
	checks := []health.HealthCheck{
		&health.PostgresCheck{PingFunc: func(context.Context) error { return nil }},
		&health.RedisCheck{PingFunc: func(context.Context) error { return nil }},
		&health.TemporalCheck{PingFunc: func(context.Context) error { return errors.New("temporal: unavailable") }},
	}
	h := health.NewHandler(checks)
	rec := httptest.NewRecorder()
	h.Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var resp health.Response
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, health.StatusNotReady, resp.Status)
	assert.Equal(t, health.CheckStatusFail, resp.Checks["temporal"].Status)
}

func TestReadiness_Timeout(t *testing.T) {
	slowCheck := &stubCheck{
		name: "slow",
		err:  nil,
	}
	hangingCheck := &stubCheck{
		name: "hanging",
	}
	hangingCheck.err = nil

	checks := []health.HealthCheck{
		slowCheck,
		&health.PostgresCheck{PingFunc: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		}},
	}
	h := health.NewHandler(checks, health.WithTimeout(50*time.Millisecond))
	rec := httptest.NewRecorder()
	h.Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestMux_WiresEndpoints(t *testing.T) {
	h := health.NewHandler(nil)
	mux := h.Mux()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

// T-5034-1: typed response structs -- verify /healthz returns LivenessResponse.

func TestLiveness_TypedResponse(t *testing.T) {
	h := health.NewHandler(nil)
	rec := httptest.NewRecorder()
	h.Liveness(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var got health.LivenessResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, health.StatusOK, got.Status)
}

func TestReadiness_TypedResponseAllPass(t *testing.T) {
	checks := []health.HealthCheck{
		&health.PostgresCheck{PingFunc: func(context.Context) error { return nil }},
	}
	h := health.NewHandler(checks)
	rec := httptest.NewRecorder()
	h.Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var got health.ReadinessResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, health.StatusReady, got.Status)
	assert.Equal(t, health.CheckStatusOK, got.Checks["postgres"].Status)
}

func TestReadiness_TypedResponseDegraded(t *testing.T) {
	checks := []health.HealthCheck{
		&health.PostgresCheck{PingFunc: func(context.Context) error { return errors.New("conn refused") }},
	}
	h := health.NewHandler(checks)
	rec := httptest.NewRecorder()
	h.Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var got health.ReadinessResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, health.StatusNotReady, got.Status)
	assert.Equal(t, health.CheckStatusFail, got.Checks["postgres"].Status)
	assert.Contains(t, got.Checks["postgres"].Error, "conn refused")
}
