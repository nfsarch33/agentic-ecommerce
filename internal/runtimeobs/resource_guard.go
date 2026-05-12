package runtimeobs

import (
	"context"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/evomap"
	"github.com/nfsarch33/agentic-ecommerce/internal/memwatch"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/observability/agentrace"
)

const (
	resourceSignalHeap           = "heap"
	resourceSignalGoroutine      = "goroutine"
	resourceSignalSentruxDesktop = "sentrux_desktop"
	resourceSeverityWarning      = "warning"
	resourceSeverityCritical     = "critical"
)

type TraceSink interface {
	Emit(context.Context, agentrace.Event) error
}

type ResourceGuardConfig struct {
	HeapWarningBytes       uint64
	HeapCriticalBytes      uint64
	GoroutineWarning       int
	GoroutineCritical      int
	SentruxDesktopWarning  int
	SentruxDesktopCritical int
	TraceSink              TraceSink
}

type ProcessSnapshot struct {
	SentruxDesktopProcesses int
}

type ResourceAlert struct {
	Signal    string
	Severity  string
	Value     float64
	Threshold float64
}

type ResourceEvaluation struct {
	Alerts                  []ResourceAlert
	AlertCount              int
	SentruxDesktopProcesses int
}

type ResourceGuard struct {
	cfg ResourceGuardConfig
}

func NewResourceGuard(cfg ResourceGuardConfig) *ResourceGuard {
	if cfg.SentruxDesktopWarning == 0 {
		cfg.SentruxDesktopWarning = 1
	}
	if cfg.SentruxDesktopCritical == 0 {
		cfg.SentruxDesktopCritical = 3
	}
	return &ResourceGuard{cfg: cfg}
}

func (g *ResourceGuard) Evaluate(sample memwatch.Sample, snapshot ProcessSnapshot) ResourceEvaluation {
	if g == nil {
		return ResourceEvaluation{SentruxDesktopProcesses: snapshot.SentruxDesktopProcesses}
	}
	alerts := make([]ResourceAlert, 0, 3)
	alerts = appendAlert(alerts, resourceSignalHeap, float64(sample.HeapInUseBytes), float64(g.cfg.HeapWarningBytes), float64(g.cfg.HeapCriticalBytes))
	alerts = appendAlert(alerts, resourceSignalGoroutine, float64(sample.GoroutineCount), float64(g.cfg.GoroutineWarning), float64(g.cfg.GoroutineCritical))
	alerts = appendAlert(alerts, resourceSignalSentruxDesktop, float64(snapshot.SentruxDesktopProcesses), float64(g.cfg.SentruxDesktopWarning), float64(g.cfg.SentruxDesktopCritical))
	return ResourceEvaluation{
		Alerts:                  alerts,
		AlertCount:              len(alerts),
		SentruxDesktopProcesses: snapshot.SentruxDesktopProcesses,
	}
}

func (rt *RuntimeObservability) ObserveResource(ctx context.Context, guard *ResourceGuard, sample memwatch.Sample, snapshot ProcessSnapshot) ResourceEvaluation {
	result := ResourceEvaluation{SentruxDesktopProcesses: snapshot.SentruxDesktopProcesses}
	if guard != nil {
		result = guard.Evaluate(sample, snapshot)
	}
	if rt == nil {
		return result
	}
	rt.emitRuntimeResourceMetrics(sample, result)
	if guard != nil {
		guard.emitTrace(ctx, sample, result)
	}
	rt.writeResourceCapsule(ctx, sample, result)
	return result
}

func (rt *RuntimeObservability) emitRuntimeResourceMetrics(sample memwatch.Sample, result ResourceEvaluation) {
	if rt.reg == nil {
		return
	}
	rt.reg.GoroutineCount.Set(float64(sample.GoroutineCount), metrics.Labels{})
	rt.reg.HeapBytes.Set(float64(sample.HeapInUseBytes), metrics.Labels{})
	rt.reg.SentruxDesktopProcessCount.Set(float64(result.SentruxDesktopProcesses), metrics.Labels{})
	for _, alert := range result.Alerts {
		rt.reg.ResourceGuardAlertsTotal.Inc(metrics.Labels{
			"signal":   alert.Signal,
			"severity": alert.Severity,
		})
	}
}

func (rt *RuntimeObservability) writeResourceCapsule(ctx context.Context, sample memwatch.Sample, result ResourceEvaluation) {
	if rt.sink == nil {
		return
	}
	recordedAt := sample.RecordedAt
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}
	cap := evomap.Capsule{
		RecordedAt: recordedAt,
		EventAt:    recordedAt,
		Binary:     sample.Binary,
		KPIs: evomap.KPIs{
			GoroutineCount:             sample.GoroutineCount,
			GCPauseP99Us:               float64(sample.GCPauseLastNs) / float64(time.Microsecond),
			HeapInUseBytes:             sample.HeapInUseBytes,
			ResourceGuardAlertsTotal:   result.AlertCount,
			SentruxDesktopProcessCount: result.SentruxDesktopProcesses,
		},
	}
	if err := rt.sink.Write(ctx, cap); err != nil {
		rt.logger.Warn("runtimeobs.resource_evomap_write_failed", "binary", sample.Binary, "error", err)
	}
}

func (g *ResourceGuard) emitTrace(ctx context.Context, sample memwatch.Sample, result ResourceEvaluation) {
	if g.cfg.TraceSink == nil {
		return
	}
	for _, alert := range result.Alerts {
		_ = g.cfg.TraceSink.Emit(ctx, agentrace.Event{
			Type:      "resource_alert",
			Timestamp: sample.RecordedAt.UTC(),
			Tool:      "runtimeobs",
			Severity:  alert.Severity,
			Ratio:     alertRatio(alert),
			Labels: map[string]string{
				"signal": alert.Signal,
			},
		})
	}
}

func appendAlert(alerts []ResourceAlert, signal string, value, warning, critical float64) []ResourceAlert {
	switch {
	case critical > 0 && value >= critical:
		return append(alerts, ResourceAlert{Signal: signal, Severity: resourceSeverityCritical, Value: value, Threshold: critical})
	case warning > 0 && value >= warning:
		return append(alerts, ResourceAlert{Signal: signal, Severity: resourceSeverityWarning, Value: value, Threshold: warning})
	default:
		return alerts
	}
}

func alertRatio(alert ResourceAlert) float64 {
	if alert.Threshold <= 0 {
		return 0
	}
	return alert.Value / alert.Threshold
}
