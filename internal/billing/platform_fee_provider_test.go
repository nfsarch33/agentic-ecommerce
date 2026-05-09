package billing

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStaticFXRateProvider_DefaultsTimestampWhenZero(t *testing.T) {
	t.Parallel()
	provider := NewStaticFXRateProvider(FXRate{AUDPerCNY: 0.21})
	rate, err := provider.LatestRate(context.Background())
	if err != nil {
		t.Fatalf("LatestRate: %v", err)
	}
	if rate.FetchedAt.IsZero() {
		t.Fatalf("LatestRate: FetchedAt = zero, want default-stamped value")
	}
	if rate.Source == "" {
		t.Fatalf("LatestRate: Source empty, want default")
	}
}

func TestFXRateFileCacheProvider_RoundTripsRate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "fx.json")
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	if err := WriteFXRateCacheFile(cachePath, FXRate{AUDPerCNY: 0.21, FetchedAt: now, Source: "test"}); err != nil {
		t.Fatalf("WriteFXRateCacheFile: %v", err)
	}
	provider, err := NewFXRateFileCacheProvider(cachePath)
	if err != nil {
		t.Fatalf("NewFXRateFileCacheProvider: %v", err)
	}
	if provider.Path() == "" {
		t.Fatalf("Path empty after construction")
	}
	rate, err := provider.LatestRate(context.Background())
	if err != nil {
		t.Fatalf("LatestRate: %v", err)
	}
	if rate.AUDPerCNY != 0.21 {
		t.Fatalf("AUDPerCNY = %f, want 0.21", rate.AUDPerCNY)
	}
	if !rate.FetchedAt.Equal(now) {
		t.Fatalf("FetchedAt = %v, want %v", rate.FetchedAt, now)
	}
	if rate.Source != "test" {
		t.Fatalf("Source = %q, want %q", rate.Source, "test")
	}
	// Second read hits the in-memory cache path.
	rate2, err := provider.LatestRate(context.Background())
	if err != nil {
		t.Fatalf("LatestRate (cached): %v", err)
	}
	if rate2.AUDPerCNY != rate.AUDPerCNY {
		t.Fatalf("cached rate diverged: %v vs %v", rate2, rate)
	}
}

func TestFXRateFileCacheProvider_RereadsAfterFileChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "fx.json")
	if err := WriteFXRateCacheFile(cachePath, FXRate{AUDPerCNY: 0.21, FetchedAt: time.Now().UTC(), Source: "v1"}); err != nil {
		t.Fatalf("WriteFXRateCacheFile v1: %v", err)
	}
	provider, err := NewFXRateFileCacheProvider(cachePath)
	if err != nil {
		t.Fatalf("NewFXRateFileCacheProvider: %v", err)
	}
	rate, err := provider.LatestRate(context.Background())
	if err != nil {
		t.Fatalf("LatestRate v1: %v", err)
	}
	if rate.Source != "v1" {
		t.Fatalf("Source v1 = %q", rate.Source)
	}
	// File-system mtime resolution on macOS APFS is 1ns; tests on
	// other filesystems may need >1s nudge. Sleep 1.1s + write.
	time.Sleep(1100 * time.Millisecond)
	if err := WriteFXRateCacheFile(cachePath, FXRate{AUDPerCNY: 0.22, FetchedAt: time.Now().UTC(), Source: "v2"}); err != nil {
		t.Fatalf("WriteFXRateCacheFile v2: %v", err)
	}
	rate2, err := provider.LatestRate(context.Background())
	if err != nil {
		t.Fatalf("LatestRate v2: %v", err)
	}
	if rate2.Source != "v2" {
		t.Fatalf("Source v2 = %q (want updated rate after mtime change)", rate2.Source)
	}
	if rate2.AUDPerCNY != 0.22 {
		t.Fatalf("AUDPerCNY v2 = %f, want 0.22", rate2.AUDPerCNY)
	}
}

func TestFXRateFileCacheProvider_RejectsMissingPath(t *testing.T) {
	t.Setenv(FXCacheEnvVar, "")
	_, err := NewFXRateFileCacheProvider("")
	if !errors.Is(err, ErrFXRateUnconfigured) {
		t.Fatalf("NewFXRateFileCacheProvider empty path: err=%v, want ErrFXRateUnconfigured", err)
	}
}

func TestFXRateFileCacheProvider_UsesEnvVarFallback(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "fx.json")
	if err := WriteFXRateCacheFile(cachePath, FXRate{AUDPerCNY: 0.21, Source: "env"}); err != nil {
		t.Fatalf("WriteFXRateCacheFile: %v", err)
	}
	t.Setenv(FXCacheEnvVar, cachePath)
	provider, err := NewFXRateFileCacheProvider("")
	if err != nil {
		t.Fatalf("NewFXRateFileCacheProvider env: %v", err)
	}
	if provider.Path() == "" {
		t.Fatalf("Path empty after env fallback")
	}
	rate, err := provider.LatestRate(context.Background())
	if err != nil {
		t.Fatalf("LatestRate: %v", err)
	}
	if rate.Source != "env" {
		t.Fatalf("Source = %q, want %q", rate.Source, "env")
	}
}

func TestFXRateFileCacheProvider_StatErrorSurfaces(t *testing.T) {
	t.Parallel()
	provider, err := NewFXRateFileCacheProvider("/nonexistent/path/that/cannot/exist/fx.json")
	if err != nil {
		t.Fatalf("NewFXRateFileCacheProvider: %v", err)
	}
	_, err = provider.LatestRate(context.Background())
	if err == nil {
		t.Fatalf("LatestRate missing file: want non-nil err")
	}
}

func TestWriteFXRateCacheFile_RejectsBadInput(t *testing.T) {
	t.Parallel()
	if err := WriteFXRateCacheFile("", FXRate{AUDPerCNY: 0.21}); err == nil {
		t.Fatalf("WriteFXRateCacheFile empty path: want err")
	}
	dir := t.TempDir()
	if err := WriteFXRateCacheFile(filepath.Join(dir, "fx.json"), FXRate{AUDPerCNY: 0}); !errors.Is(err, ErrInvalidPriceComponents) {
		t.Fatalf("WriteFXRateCacheFile zero rate: err=%v, want ErrInvalidPriceComponents", err)
	}
}
