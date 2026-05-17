package main

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const defaultDependencyTimeout = 5 * time.Second

func (s *server) dependencyTimeout() time.Duration {
	if s != nil && s.cfg.dependencyTimeout > 0 {
		return s.cfg.dependencyTimeout
	}
	return defaultDependencyTimeout
}

func (s *server) withDependencyDeadline(r *http.Request) (*http.Request, context.CancelFunc) {
	timeout := s.dependencyTimeout()
	if timeout <= 0 {
		return r, func() {}
	}
	deadline := time.Now().Add(timeout)
	if existing, ok := r.Context().Deadline(); ok && !existing.After(deadline) {
		return r, func() {}
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	return r.WithContext(ctx), cancel
}

func isDependencyTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func writeDependencyTimeout(w http.ResponseWriter) {
	writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "dependency_timeout"})
}
