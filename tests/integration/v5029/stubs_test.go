package v5029

import (
	"context"
	"errors"
)

var errStubFail = errors.New("stub: dependency unavailable")

type stubHealthCheck struct {
	name string
	err  error
}

func (s *stubHealthCheck) Name() string { return s.name }
func (s *stubHealthCheck) Check(_ context.Context) error {
	return s.err
}
