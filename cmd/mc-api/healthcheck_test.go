package main

import (
	"net"
	"net/http"
	"testing"
)

func TestRunHealthcheckUsesLoopbackForWildcardAddress(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(healthzHandler)}
	t.Cleanup(func() {
		_ = server.Close()
	})
	go func() {
		_ = server.Serve(listener)
	}()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	if err := runHealthcheck("0.0.0.0:" + port); err != nil {
		t.Fatalf("runHealthcheck: %v", err)
	}
}

func TestRunHealthcheckReportsUnhealthyStatus(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})}
	t.Cleanup(func() {
		_ = server.Close()
	})
	go func() {
		_ = server.Serve(listener)
	}()

	if err := runHealthcheck(listener.Addr().String()); err == nil {
		t.Fatal("runHealthcheck accepted non-200 health response")
	}
}
