package metrics

import (
	"strings"
	"testing"
)

func TestV8ResourceMetricsRegisteredAndExposed(t *testing.T) {
	t.Parallel()

	r := NewRegistry("mc-api")
	r.ResourceGuardAlertsTotal.Inc(Labels{"signal": "heap", "severity": "critical"})
	r.SentruxDesktopProcessCount.Set(2, Labels{})
	r.WorkerpoolSize.Set(6, Labels{"pool": "image-edit"})
	r.WorkerpoolResizeTotal.Inc(Labels{"pool": "image-edit", "direction": "shrink"})

	body := scrape(t, r)
	for _, want := range []string{
		"ec_resource_guard_alerts_total",
		"ec_sentrux_desktop_process_count",
		"ec_workerpool_size",
		"ec_workerpool_resize_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q\n%s", want, body)
		}
	}
}
