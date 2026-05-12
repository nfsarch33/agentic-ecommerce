package runtimeobs

import (
	"errors"
	"strings"
	"testing"
)

func TestLoadProcessSnapshotFromResourceProbeUsesLatestSanitizedSample(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		`{"recorded_at":"2026-05-12T14:00:00Z","memory_free_percent":38,"sentrux_desktop_processes":2,"sentrux_mcp_processes":1}`,
		`{"recorded_at":"2026-05-12T14:05:00Z","memory_free_percent":44,"sentrux_desktop_processes":0,"sentrux_mcp_processes":2}`,
	}, "\n")

	snapshot, err := LoadProcessSnapshotFromResourceProbe(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("LoadProcessSnapshotFromResourceProbe: %v", err)
	}
	if snapshot.SentruxDesktopProcesses != 0 {
		t.Fatalf("SentruxDesktopProcesses=%d, want 0", snapshot.SentruxDesktopProcesses)
	}
	if snapshot.SentruxMCPProcesses != 2 {
		t.Fatalf("SentruxMCPProcesses=%d, want 2", snapshot.SentruxMCPProcesses)
	}
	if snapshot.MemoryFreePercent != 44 {
		t.Fatalf("MemoryFreePercent=%.1f, want 44", snapshot.MemoryFreePercent)
	}
}

func TestLoadProcessSnapshotFromResourceProbeAcceptsRunxFreePct(t *testing.T) {
	t.Parallel()

	raw := `{"ts":"2026-05-13T01:20:18+10:00","event":"memory_pressure_probe","summary":"System-wide memory free percentage: 46%","free_pct":46}`

	snapshot, err := LoadProcessSnapshotFromResourceProbe(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("LoadProcessSnapshotFromResourceProbe: %v", err)
	}
	if snapshot.MemoryFreePercent != 46 {
		t.Fatalf("MemoryFreePercent=%.1f, want 46", snapshot.MemoryFreePercent)
	}
}

func TestLoadProcessSnapshotFromResourceProbeRejectsRawProcessCommand(t *testing.T) {
	t.Parallel()

	raw := `{"recorded_at":"2026-05-12T14:00:00Z","sentrux_desktop_processes":1,"process_cmdline":"Sentrux.app/Contents/MacOS/sentrux"}`

	_, err := LoadProcessSnapshotFromResourceProbe(strings.NewReader(raw))
	if !errors.Is(err, ErrUnsafeResourceProbe) {
		t.Fatalf("err=%v, want ErrUnsafeResourceProbe", err)
	}
}
